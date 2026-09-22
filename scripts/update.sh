#!/usr/bin/env bash
# Update an installed Tracker, with automatic rollback.
#
#   sudo /opt/tracker/scripts/update.sh                       # latest release of the configured repo
#   sudo /opt/tracker/scripts/update.sh --version v0.2.0
#   sudo /opt/tracker/scripts/update.sh --url https://…/tracker-v0.2.0-linux-amd64.tar.gz
#   sudo /opt/tracker/scripts/update.sh --file ./tracker-v0.2.0-linux-amd64.tar.gz   (or a bare binary)
#
# The GitHub repository is read from --repo or /etc/tracker/release.conf (TRACKER_REPO=owner/name).
set -euo pipefail

BIN_PATH=/usr/local/bin/tracker
ENV_FILE=/etc/tracker/tracker.env
RELEASE_CONF=/etc/tracker/release.conf
OPT_DIR=/opt/tracker

VERSION=latest
URL=""
FILE=""
REPO=""

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION=${2:?}; shift 2 ;;
    --url) URL=${2:?}; shift 2 ;;
    --file) FILE=${2:?}; shift 2 ;;
    --repo) REPO=${2:?}; shift 2 ;;
    -h|--help) sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root"
[ -x "$BIN_PATH" ] || die "$BIN_PATH is not installed; run setup.sh first"
[ -f "$ENV_FILE" ] || die "$ENV_FILE not found; run setup.sh first"

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported CPU architecture $(uname -m)" ;;
esac

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# --- obtain ---------------------------------------------------------------------

if [ -z "$FILE" ] && [ -z "$URL" ]; then
  if [ -z "$REPO" ] && [ -f "$RELEASE_CONF" ]; then
    REPO=$(sed -n 's/^TRACKER_REPO=//p' "$RELEASE_CONF")
  fi
  [ -n "$REPO" ] || die "no release source: pass --file, --url or --repo OWNER/NAME"
  api="https://api.github.com/repos/$REPO/releases/latest"
  [ "$VERSION" = latest ] || api="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
  meta=$(curl -fsSL "$api") || die "cannot query $api"
  URL=$(printf '%s' "$meta" | grep -o "https://[^\"]*linux-$ARCH\.tar\.gz" | head -n1) || true
  SUMS_URL=$(printf '%s' "$meta" | grep -o "https://[^\"]*SHA256SUMS" | head -n1) || true
  [ -n "$URL" ] || die "no linux-$ARCH asset in release '$VERSION' of $REPO"
fi

if [ -n "$URL" ]; then
  FILE=$WORK/$(basename "$URL")
  log "Downloading $URL"
  curl -fsSL --retry 3 -o "$FILE" "$URL" || die "download failed"
  SUMS_URL=${SUMS_URL:-$(dirname "$URL")/SHA256SUMS}
  if curl -fsSL --retry 2 -o "$WORK/SHA256SUMS" "$SUMS_URL" 2>/dev/null; then
    expected=$(awk -v f="$(basename "$FILE")" '$2 == f || $2 == "*"f {print $1}' "$WORK/SHA256SUMS")
    [ -n "$expected" ] || die "$(basename "$FILE") is not listed in SHA256SUMS"
    actual=$(sha256sum "$FILE" | awk '{print $1}')
    [ "$expected" = "$actual" ] || die "checksum mismatch for $(basename "$FILE")"
    log "Checksum verified"
  else
    warn "no SHA256SUMS next to the release; skipping checksum verification"
  fi
fi

[ -f "$FILE" ] || die "file not found: $FILE"

# --- validate -------------------------------------------------------------------

NEW_SCRIPTS=""
if tar -tzf "$FILE" >/dev/null 2>&1; then
  mkdir -p "$WORK/release"
  tar -xzf "$FILE" -C "$WORK/release" --strip-components=1 --no-same-owner || die "cannot extract $FILE"
  NEW_BIN=$WORK/release/tracker
  [ -d "$WORK/release/scripts" ] && NEW_SCRIPTS=$WORK/release
else
  NEW_BIN=$FILE
fi
[ -f "$NEW_BIN" ] || die "the release does not contain a 'tracker' binary"
chmod +x "$NEW_BIN"
NEW_VERSION=$("$NEW_BIN" version 2>/dev/null) || die "the new binary does not run on this machine (wrong architecture or corrupt download)"
OLD_VERSION=$("$BIN_PATH" version 2>/dev/null || echo unknown)

if cmp -s "$NEW_BIN" "$BIN_PATH"; then
  log "Already running $OLD_VERSION; nothing to do"
  exit 0
fi
log "Updating $OLD_VERSION → $NEW_VERSION"

# --- safety net -----------------------------------------------------------------

get_env() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1; }
addr=$(get_env TRACKER_ADDR); port=${addr##*:}
data_dir=$(get_env TRACKER_DATA_DIR); data_dir=${data_dir:-/var/lib/tracker}
health_url="http://127.0.0.1:${port:-3000}/health"

wait_healthy() {
  for _ in $(seq 1 30); do
    curl -fsS --max-time 2 "$health_url" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

# A new version may migrate the schema, and an older binary cannot read a newer
# schema — so a binary rollback is only safe together with this snapshot.
snapshot_dir=$data_dir/backups
snapshot=$snapshot_dir/pre-update-$(date -u +%Y%m%d-%H%M%S).db
install -d -m 0750 -o tracker -g tracker "$snapshot_dir"
log "Snapshotting the database to $snapshot"
# Run as the service user so SQLite's -wal/-shm files never end up owned by root.
runuser -u tracker -- sh -c 'set -a; . "$1"; set +a; shift; exec "$@"' sh "$ENV_FILE" "$BIN_PATH" backup "$snapshot" \
  || die "database snapshot failed; not updating"
# keep the three most recent snapshots
ls -1t "$snapshot_dir"/pre-update-*.db 2>/dev/null | tail -n +4 | xargs -r rm -f

cp -p "$BIN_PATH" "$BIN_PATH.previous"

# --- swap -----------------------------------------------------------------------

install -m 0755 -o root -g root "$NEW_BIN" "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"
log "Restarting tracker (migrations run on startup)"
systemctl restart tracker || true

if wait_healthy; then
  if [ -n "$NEW_SCRIPTS" ]; then
    install -m 0755 "$NEW_SCRIPTS"/scripts/*.sh "$OPT_DIR/scripts/"
    [ -d "$NEW_SCRIPTS/deploy" ] && install -m 0644 "$NEW_SCRIPTS"/deploy/* "$OPT_DIR/deploy/"
  fi
  log "Tracker $NEW_VERSION is healthy. Previous binary kept at $BIN_PATH.previous"
  exit 0
fi

# --- rollback -------------------------------------------------------------------

warn "new version failed its health check; rolling back to $OLD_VERSION"
journalctl -u tracker --no-pager -n 25 || true
systemctl stop tracker || true
mv -f "$BIN_PATH.previous" "$BIN_PATH"
db=$(get_env TRACKER_DATABASE_URL); db=${db:-$data_dir/tracker.db}
# Restore the pre-update snapshot in case the failed version already migrated.
install -m 0640 -o tracker -g tracker "$snapshot" "$db.rollback"
rm -f "$db-wal" "$db-shm"
mv -f "$db.rollback" "$db"
systemctl start tracker || true
if wait_healthy; then
  die "update failed; rolled back to $OLD_VERSION (service is healthy)"
fi
die "update failed AND the rollback is unhealthy — inspect: journalctl -u tracker"
