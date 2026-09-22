#!/usr/bin/env bash
# Create a consistent backup of an installed Tracker while it keeps running.
#
#   sudo /opt/tracker/scripts/backup.sh [--output-dir DIR] [--include-secrets] [--keep N]
#
# The database is copied with SQLite's VACUUM INTO (through `tracker backup`),
# never by copying a live WAL database file. Result:
#   DIR/tracker-backup-YYYY-MM-DD-HHMMSS.tar.gz
#     tracker.db            consistent snapshot
#     tracker.env           configuration, TRACKER_SESSION_SECRET redacted
#     uploads/              only if the data directory has one
#     MANIFEST              version, time, host
# With --include-secrets the archive also carries the unredacted env file and
# the generated session_secret, so a restore keeps everyone signed in. Treat
# such an archive like a password.
set -euo pipefail

BIN_PATH=/usr/local/bin/tracker
ENV_FILE=${TRACKER_ENV_FILE:-/etc/tracker/tracker.env}
OUTPUT_DIR=/var/backups/tracker
INCLUDE_SECRETS=0
KEEP=0

die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --output-dir) OUTPUT_DIR=${2:?}; shift 2 ;;
    --include-secrets) INCLUDE_SECRETS=1; shift ;;
    --keep) KEEP=${2:?}; shift 2 ;;
    -h|--help) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root"
[ -x "$BIN_PATH" ] || die "$BIN_PATH not found"
[ -f "$ENV_FILE" ] || die "$ENV_FILE not found"

get_env() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1; }
data_dir=$(get_env TRACKER_DATA_DIR); data_dir=${data_dir:-/var/lib/tracker}

stamp=$(date +%Y-%m-%d-%H%M%S)
name=tracker-backup-$stamp
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/$name"
chown tracker:tracker "$stage" "$stage/$name"
chmod 0750 "$stage" "$stage/$name"

# As the service user: a root-owned -wal/-shm file would lock the service out.
runuser -u tracker -- sh -c 'set -a; . "$1"; set +a; shift; exec "$@"' sh "$ENV_FILE" \
  "$BIN_PATH" backup "$stage/$name/tracker.db" || die "database snapshot failed"
"$BIN_PATH" check "$stage/$name/tracker.db" >/dev/null || die "snapshot failed verification"

if [ "$INCLUDE_SECRETS" -eq 1 ]; then
  cp "$ENV_FILE" "$stage/$name/tracker.env"
  [ -f "$data_dir/session_secret" ] && cp "$data_dir/session_secret" "$stage/$name/session_secret"
else
  sed 's/^\(TRACKER_SESSION_SECRET\)=.*/\1=/' "$ENV_FILE" > "$stage/$name/tracker.env"
fi
[ -d "$data_dir/uploads" ] && cp -a "$data_dir/uploads" "$stage/$name/uploads"

cat > "$stage/$name/MANIFEST" <<MANIFEST
tracker_version=$("$BIN_PATH" version)
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
host=$(hostname)
includes_secrets=$INCLUDE_SECRETS
MANIFEST

install -d -m 0700 "$OUTPUT_DIR"
archive=$OUTPUT_DIR/$name.tar.gz
( umask 077; tar -C "$stage" --owner=0 --group=0 -czf "$archive" "$name" )

if [ "$KEEP" -gt 0 ]; then
  ls -1t "$OUTPUT_DIR"/tracker-backup-*.tar.gz | tail -n +"$((KEEP + 1))" | xargs -r rm -f
fi

echo "$archive"
