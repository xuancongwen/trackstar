#!/usr/bin/env bash
# Restore a backup made by backup.sh.
#
#   sudo /opt/tracker/scripts/restore.sh /var/backups/tracker/tracker-backup-….tar.gz [--with-config]
#
# The current data is moved to <data-dir>/pre-restore-<timestamp>/ first and is
# put back automatically if the restored service does not become healthy.
# The environment file is only replaced with --with-config, and only from an
# archive created with --include-secrets.
set -euo pipefail

BIN_PATH=/usr/local/bin/tracker
ENV_FILE=${TRACKER_ENV_FILE:-/etc/tracker/tracker.env}
ARCHIVE=""
WITH_CONFIG=0

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --with-config) WITH_CONFIG=1; shift ;;
    -h|--help) sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) die "unknown option: $1" ;;
    *) [ -z "$ARCHIVE" ] || die "only one archive may be given"; ARCHIVE=$1; shift ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root"
[ -n "$ARCHIVE" ] || die "usage: restore.sh <tracker-backup-….tar.gz> [--with-config]"
[ -f "$ARCHIVE" ] || die "archive not found: $ARCHIVE"
[ -x "$BIN_PATH" ] || die "$BIN_PATH not found; run setup.sh first"
[ -f "$ENV_FILE" ] || die "$ENV_FILE not found; run setup.sh first"

# --- validate before touching anything --------------------------------------------

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

tar -tzf "$ARCHIVE" >/dev/null 2>&1 || die "not a readable .tar.gz archive"
if tar -tzf "$ARCHIVE" | grep -qE '(^|/)\.\.(/|$)|^/'; then
  die "archive contains unsafe paths"
fi
tar -xzf "$ARCHIVE" -C "$stage" --no-same-owner
src=$(find "$stage" -mindepth 1 -maxdepth 1 -type d | head -n1)
[ -n "$src" ] && [ -f "$src/tracker.db" ] || die "archive does not contain tracker.db"
check=$("$BIN_PATH" check "$src/tracker.db") || die "tracker.db in the archive is corrupt or not a tracker database"
log "Archive verified ($check)"
[ -f "$src/MANIFEST" ] && sed 's/^/    /' "$src/MANIFEST"

if [ "$WITH_CONFIG" -eq 1 ]; then
  grep -q '^includes_secrets=1' "$src/MANIFEST" 2>/dev/null \
    || die "--with-config needs an archive created with backup.sh --include-secrets"
fi

get_env() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1; }
data_dir=$(get_env TRACKER_DATA_DIR); data_dir=${data_dir:-/var/lib/tracker}
db=$(get_env TRACKER_DATABASE_URL); db=${db:-$data_dir/tracker.db}

# --- swap ---------------------------------------------------------------------------

log "Stopping tracker"
systemctl stop tracker

emergency=$data_dir/pre-restore-$(date +%Y%m%d-%H%M%S)
install -d -m 0750 -o tracker -g tracker "$emergency"
for f in "$db" "$db-wal" "$db-shm"; do
  [ -e "$f" ] && mv "$f" "$emergency/"
done
[ -d "$data_dir/uploads" ] && mv "$data_dir/uploads" "$emergency/uploads"
log "Current data preserved in $emergency"

install -m 0640 -o tracker -g tracker "$src/tracker.db" "$db"
if [ -d "$src/uploads" ]; then
  cp -a "$src/uploads" "$data_dir/uploads"
  chown -R tracker:tracker "$data_dir/uploads"
fi
if [ "$WITH_CONFIG" -eq 1 ]; then
  cp -p "$ENV_FILE" "$emergency/tracker.env"
  install -m 0640 -o root -g tracker "$src/tracker.env" "$ENV_FILE"
  [ -f "$src/session_secret" ] && install -m 0600 -o tracker -g tracker "$src/session_secret" "$data_dir/session_secret"
fi
chown tracker:tracker "$data_dir"

addr=$(get_env TRACKER_ADDR); port=${addr##*:}
health_url="http://127.0.0.1:${port:-3000}/health"
wait_healthy() {
  for _ in $(seq 1 30); do
    curl -fsS --max-time 2 "$health_url" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

log "Starting tracker"
systemctl start tracker || true
if wait_healthy; then
  log "Restore complete and healthy. Remove $emergency once you are satisfied."
  exit 0
fi

warn "restored data did not come up healthy; putting the previous data back"
journalctl -u tracker --no-pager -n 25 || true
systemctl stop tracker || true
rm -f "$db" "$db-wal" "$db-shm"
for f in "$emergency"/"$(basename "$db")"*; do
  [ -e "$f" ] && mv "$f" "$(dirname "$db")/"
done
if [ -d "$emergency/uploads" ]; then
  rm -rf "$data_dir/uploads"
  mv "$emergency/uploads" "$data_dir/uploads"
fi
[ -f "$emergency/tracker.env" ] && install -m 0640 -o root -g tracker "$emergency/tracker.env" "$ENV_FILE"
systemctl start tracker || true
wait_healthy && die "restore failed; the previous data is back in place and healthy"
die "restore failed and the previous data is unhealthy too — inspect: journalctl -u tracker"
