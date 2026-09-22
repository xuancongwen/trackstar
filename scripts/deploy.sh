#!/usr/bin/env bash
# Build locally and deploy to a host over SSH.
#
#   ./scripts/deploy.sh root@192.168.1.240
#   ./scripts/deploy.sh --skip-build deploy@track.example.com      (needs passwordless sudo)
#   ./scripts/deploy.sh root@new-host -- --public-url https://track.example.com/
#
# On a host without Tracker this performs the first installation through
# setup.sh (arguments after `--` are passed to it). On an installed host it
# only swaps the binary: /etc/tracker/tracker.env is never touched, the old
# binary is kept as tracker.previous and is restored if the health check fails.
set -euo pipefail

SKIP_BUILD=0
TARGET=""
SETUP_ARGS=()

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-build) SKIP_BUILD=1; shift ;;
    -h|--help) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    --) shift; SETUP_ARGS=("$@"); break ;;
    -*) die "unknown option: $1" ;;
    *) [ -z "$TARGET" ] || die "only one target host may be given"; TARGET=$1; shift ;;
  esac
done
[ -n "$TARGET" ] || die "usage: deploy.sh [--skip-build] user@host [-- setup.sh options]"

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

SSH_OPTS=(-o ConnectTimeout=10 -o ControlMaster=auto -o ControlPersist=60 -o "ControlPath=/tmp/tracker-deploy-%C")
remote() { ssh "${SSH_OPTS[@]}" "$TARGET" "$@"; }

log "Checking $TARGET"
remote_info=$(remote 'echo "$(uname -s) $(uname -m) $(id -u)"') || die "cannot connect to $TARGET"
read -r r_os r_machine r_uid <<<"$remote_info"
[ "$r_os" = Linux ] || die "remote OS is $r_os; only Linux is supported"
case "$r_machine" in
  x86_64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) die "unsupported remote architecture: $r_machine" ;;
esac
SUDO=""
if [ "$r_uid" != 0 ]; then
  remote 'sudo -n true' 2>/dev/null || die "$TARGET is not root and has no passwordless sudo"
  SUDO="sudo -n"
fi

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
OUT=$ROOT_DIR/bin/tracker-linux-$GOARCH

if [ "$SKIP_BUILD" -eq 0 ]; then
  log "Building frontend"
  [ -d web/node_modules ] || npm --prefix web ci --silent
  npm --prefix web run build --silent
  log "Building tracker $VERSION for linux/$GOARCH"
  CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o "$OUT" ./cmd/tracker
fi
[ -f "$OUT" ] || die "$OUT not found (build it, or drop --skip-build)"

log "Uploading"
STAGE=$(remote 'mktemp -d /tmp/tracker-deploy.XXXXXX')
# shellcheck disable=SC2064
trap "ssh ${SSH_OPTS[*]} $TARGET 'rm -rf $STAGE' >/dev/null 2>&1 || true" EXIT
tar -C "$ROOT_DIR" -czf - scripts deploy -C "$(dirname "$OUT")" "$(basename "$OUT")" \
  | remote "tar -xzf - --no-same-owner -C '$STAGE' && mv '$STAGE/$(basename "$OUT")' '$STAGE/tracker'"

if ! remote "test -f /etc/tracker/tracker.env && test -x /usr/local/bin/tracker"; then
  log "Tracker is not installed on $TARGET yet: running setup.sh"
  setup_args=""
  for a in "${SETUP_ARGS[@]+"${SETUP_ARGS[@]}"}"; do setup_args+=" $(printf '%q' "$a")"; done
  remote "$SUDO bash '$STAGE/scripts/setup.sh' --binary '$STAGE/tracker'$setup_args"
  exit 0
fi

log "Swapping binary on $TARGET"
remote "$SUDO env STAGE='$STAGE' bash -s" <<'REMOTE'
set -euo pipefail
BIN=/usr/local/bin/tracker
ENV_FILE=/etc/tracker/tracker.env
say() { printf '    %s\n' "$*"; }

get_env() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1; }
addr=$(get_env TRACKER_ADDR); port=${addr##*:}
data_dir=$(get_env TRACKER_DATA_DIR); data_dir=${data_dir:-/var/lib/tracker}
db=$(get_env TRACKER_DATABASE_URL); db=${db:-$data_dir/tracker.db}
health="http://127.0.0.1:${port:-3000}/health"
wait_healthy() {
  for _ in $(seq 1 30); do
    curl -fsS --max-time 2 "$health" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

"$STAGE/tracker" version >/dev/null || { echo "uploaded binary does not run here" >&2; exit 1; }
old=$("$BIN" version 2>/dev/null || echo unknown); new=$("$STAGE/tracker" version)

# Snapshot first: the new version may migrate the schema (see update.sh).
install -d -m 0750 -o tracker -g tracker "$data_dir/backups"
snapshot=$data_dir/backups/pre-deploy-$(date -u +%Y%m%d-%H%M%S).db
runuser -u tracker -- sh -c 'set -a; . "$1"; set +a; shift; exec "$@"' sh "$ENV_FILE" "$BIN" backup "$snapshot"
ls -1t "$data_dir"/backups/pre-deploy-*.db 2>/dev/null | tail -n +4 | xargs -r rm -f
say "database snapshot: $snapshot"

install -m 0755 -o root -g root "$STAGE/tracker" "$BIN.new"
say "stopping tracker"
systemctl stop tracker
cp -p "$BIN" "$BIN.previous"
mv -f "$BIN.new" "$BIN"                      # rename(2): atomic
say "starting tracker $new (migrations run on startup)"
systemctl start tracker || true

if wait_healthy; then
  install -d /opt/tracker/scripts /opt/tracker/deploy
  install -m 0755 "$STAGE"/scripts/*.sh /opt/tracker/scripts/
  install -m 0644 "$STAGE"/deploy/* /opt/tracker/deploy/
  say "healthy: $old → $new   (rollback binary: $BIN.previous)"
  exit 0
fi

echo "new version failed its health check; rolling back to $old" >&2
journalctl -u tracker --no-pager -n 25 >&2 || true
systemctl stop tracker || true
mv -f "$BIN.previous" "$BIN"
rm -f "$db-wal" "$db-shm"
install -m 0640 -o tracker -g tracker "$snapshot" "$db"
systemctl start tracker || true
wait_healthy && echo "rolled back; service is healthy on $old" >&2
exit 1
REMOTE

log "Deployed $VERSION to $TARGET"
remote "$SUDO systemctl --no-pager --lines=0 status tracker" || true
