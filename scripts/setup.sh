#!/usr/bin/env bash
# Install Trackstar natively on a fresh Debian/Ubuntu machine (droplet, VM, LXC).
# Safe to re-run: existing configuration, secrets and data are kept.
#
#   sudo ./setup.sh --public-url https://track.example.com/ --port 3000
#
# The binary comes from (first match wins):
#   --binary PATH          a trackstar executable
#   ../trackstar             when run from an extracted release archive
#   ../bin/trackstar         when run from a source checkout after `make build`
#   --release-url URL      a release .tar.gz to download
#   --repo OWNER/NAME      the latest (or --version TAG) GitHub release
set -euo pipefail

PUBLIC_URL=""
PORT=""
BIND_HOST=""
BINARY=""
RELEASE_URL=""
REPO=""
VERSION="latest"
TIMEZONE=""
ALLOW_REGISTRATION=""
START=1

BIN_PATH=/usr/local/bin/trackstar
ETC_DIR=/etc/trackstar
ENV_FILE=$ETC_DIR/trackstar.env
DATA_DIR=/var/lib/trackstar
OPT_DIR=/opt/trackstar
UNIT=/etc/systemd/system/trackstar.service
DROPIN_DIR=/etc/systemd/system/trackstar.service.d

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(dirname "$SCRIPT_DIR")

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; cat <<'USAGE'

Options:
  --public-url URL            external URL (default http://<this-ip>:<port>/)
  --port N                    listen port (default 3000)
  --bind HOST                 listen host (default 0.0.0.0)
  --timezone ZONE             IANA zone for iteration boundaries (default UTC)
  --allow-registration BOOL   true|false (default true)
  --binary PATH | --release-url URL | --repo OWNER/NAME [--version TAG]
  --no-start                  install everything but do not start the service
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --public-url) PUBLIC_URL=${2:?}; shift 2 ;;
    --port) PORT=${2:?}; shift 2 ;;
    --bind) BIND_HOST=${2:?}; shift 2 ;;
    --binary) BINARY=${2:?}; shift 2 ;;
    --release-url) RELEASE_URL=${2:?}; shift 2 ;;
    --repo) REPO=${2:?}; shift 2 ;;
    --version) VERSION=${2:?}; shift 2 ;;
    --timezone) TIMEZONE=${2:?}; shift 2 ;;
    --allow-registration) ALLOW_REGISTRATION=${2:?}; shift 2 ;;
    --no-start) START=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

# --- preflight ------------------------------------------------------------------

[ "$(id -u)" -eq 0 ] || die "run as root (sudo $0 ...)"
[ -r /etc/os-release ] || die "/etc/os-release not found; only Debian and Ubuntu are supported"
# shellcheck disable=SC1091
. /etc/os-release
case " ${ID:-} ${ID_LIKE:-} " in
  *" debian "*|*" ubuntu "*) ;;
  *) die "unsupported distribution '${PRETTY_NAME:-unknown}'; only Debian and Ubuntu are supported" ;;
esac
command -v systemctl >/dev/null || die "systemd is required"
if [ -n "$PORT" ]; then
  case "$PORT" in *[!0-9]*) die "--port must be a number" ;; esac
  [ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ] || die "--port must be between 1 and 65535"
fi
if [ -n "$PUBLIC_URL" ]; then
  case "$PUBLIC_URL" in http://*|https://*) ;; *) die "--public-url must start with http:// or https://" ;; esac
fi

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported CPU architecture $(uname -m)" ;;
esac

log "Installing on ${PRETTY_NAME} (${ARCH})"

# --- packages -------------------------------------------------------------------

missing=()
for pkg in ca-certificates curl tar; do
  dpkg -s "$pkg" >/dev/null 2>&1 || missing+=("$pkg")
done
if [ ${#missing[@]} -gt 0 ]; then
  log "Installing packages: ${missing[*]}"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends "${missing[@]}" >/dev/null
fi

# --- locate the binary ----------------------------------------------------------

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

fetch_release() { # url → extracts into $WORK/release, sets BINARY
  log "Downloading $1"
  curl -fsSL --retry 3 -o "$WORK/release.tar.gz" "$1" || die "download failed: $1"
  mkdir -p "$WORK/release"
  tar -xzf "$WORK/release.tar.gz" -C "$WORK/release" --strip-components=1 --no-same-owner || die "cannot extract the release archive"
  BINARY=$WORK/release/trackstar
  # Prefer the scripts and unit file that ship with the downloaded version.
  [ -d "$WORK/release/scripts" ] && ROOT_DIR=$WORK/release
}

if [ -z "$BINARY" ]; then
  if [ -x "$ROOT_DIR/trackstar" ]; then
    BINARY=$ROOT_DIR/trackstar
  elif [ -x "$ROOT_DIR/bin/trackstar" ]; then
    BINARY=$ROOT_DIR/bin/trackstar
  elif [ -n "$RELEASE_URL" ]; then
    fetch_release "$RELEASE_URL"
  elif [ -n "$REPO" ]; then
    api="https://api.github.com/repos/$REPO/releases/latest"
    [ "$VERSION" = latest ] || api="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
    url=$(curl -fsSL "$api" | grep -o "https://[^\"]*linux-$ARCH\.tar\.gz" | head -n1) || true
    [ -n "$url" ] || die "no linux-$ARCH release asset found for $REPO ($VERSION)"
    fetch_release "$url"
  elif [ -x "$BIN_PATH" ]; then
    log "No new binary supplied; keeping the installed $BIN_PATH"
    BINARY=$BIN_PATH
  else
    die "no trackstar binary found; pass --binary, --release-url or --repo"
  fi
fi
[ -f "$BINARY" ] || die "binary not found: $BINARY"
NEW_VERSION=$("$BINARY" version 2>/dev/null) || die "$BINARY does not run on this machine (wrong architecture?)"
log "Trackstar version: $NEW_VERSION"

# --- user, directories ----------------------------------------------------------

if ! id trackstar >/dev/null 2>&1; then
  log "Creating system user 'trackstar'"
  useradd --system --home-dir "$DATA_DIR" --no-create-home --shell /usr/sbin/nologin trackstar
fi
install -d -m 0750 -o root    -g trackstar "$ETC_DIR"
install -d -m 0750 -o trackstar -g trackstar "$DATA_DIR"
install -d -m 0755 "$OPT_DIR" "$OPT_DIR/scripts" "$OPT_DIR/deploy"

# --- binary, scripts ------------------------------------------------------------

WAS_ACTIVE=0
systemctl is-active --quiet trackstar 2>/dev/null && WAS_ACTIVE=1

if [ "$BINARY" != "$BIN_PATH" ]; then
  if [ -x "$BIN_PATH" ] && ! cmp -s "$BINARY" "$BIN_PATH"; then
    cp -p "$BIN_PATH" "$BIN_PATH.previous"
  fi
  # install to a temp name + rename = atomic replacement, even while running
  install -m 0755 -o root -g root "$BINARY" "$BIN_PATH.new"
  mv -f "$BIN_PATH.new" "$BIN_PATH"
  log "Installed $BIN_PATH"
fi

for f in "$ROOT_DIR"/scripts/*.sh; do
  [ -f "$f" ] && install -m 0755 "$f" "$OPT_DIR/scripts/"
done
for f in "$ROOT_DIR"/deploy/*; do
  [ -f "$f" ] && install -m 0644 "$f" "$OPT_DIR/deploy/"
done
if [ -n "$REPO" ]; then
  printf 'TRACKSTAR_REPO=%s\n' "$REPO" > "$ETC_DIR/release.conf"
fi

# --- configuration --------------------------------------------------------------

set_env() { # KEY VALUE — replace or append in $ENV_FILE
  local key=$1 value=$2 escaped
  escaped=$(printf '%s' "$value" | sed 's/[\\&|]/\\&/g')
  if grep -q "^$key=" "$ENV_FILE"; then
    sed -i "s|^$key=.*|$key=$escaped|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}
get_env() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1; }

if [ ! -f "$ENV_FILE" ]; then
  log "Creating $ENV_FILE"
  port=${PORT:-3000}
  host_ip=$(hostname -I 2>/dev/null | awk '{print $1}') || true
  umask 027
  cat > "$ENV_FILE" <<ENV
# Trackstar configuration. Documented in $OPT_DIR/deploy/trackstar.env.example
TRACKSTAR_ADDR=${BIND_HOST:-0.0.0.0}:$port
TRACKSTAR_DATA_DIR=$DATA_DIR
TRACKSTAR_DATABASE_DRIVER=sqlite
TRACKSTAR_DATABASE_URL=$DATA_DIR/trackstar.db
TRACKSTAR_PUBLIC_URL=${PUBLIC_URL:-http://${host_ip:-localhost}:$port/}
TRACKSTAR_ALLOW_REGISTRATION=${ALLOW_REGISTRATION:-true}
TRACKSTAR_SESSION_SECRET=
TRACKSTAR_LOG_LEVEL=info
TRACKSTAR_TIMEZONE=${TIMEZONE:-UTC}
TRACKSTAR_TRUSTED_PROXIES=127.0.0.0/8,::1/128
ENV
else
  log "Keeping existing $ENV_FILE (only explicitly passed options are updated)"
  if [ -n "$PORT" ] || [ -n "$BIND_HOST" ]; then
    current=$(get_env TRACKSTAR_ADDR)
    set_env TRACKSTAR_ADDR "${BIND_HOST:-${current%:*}}:${PORT:-${current##*:}}"
  fi
  [ -z "$PUBLIC_URL" ] || set_env TRACKSTAR_PUBLIC_URL "$PUBLIC_URL"
  [ -z "$TIMEZONE" ] || set_env TRACKSTAR_TIMEZONE "$TIMEZONE"
  [ -z "$ALLOW_REGISTRATION" ] || set_env TRACKSTAR_ALLOW_REGISTRATION "$ALLOW_REGISTRATION"
fi

if [ -z "$(get_env TRACKSTAR_SESSION_SECRET)" ]; then
  log "Generating session secret"
  set_env TRACKSTAR_SESSION_SECRET "$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
fi
chown root:trackstar "$ENV_FILE"
chmod 0640 "$ENV_FILE"

# --- systemd --------------------------------------------------------------------

install -m 0644 "$ROOT_DIR/deploy/trackstar.service" "$UNIT"

# Unprivileged containers without nesting cannot create mount namespaces, which
# ProtectSystem=/PrivateTmp= & co. need (the service would die with 226/NAMESPACE).
# Probe for it instead of guessing, and relax only what cannot work.
if systemd-run --quiet --wait --collect -p PrivateTmp=yes -p ProtectSystem=strict -p PrivateDevices=yes true 2>/dev/null; then
  rm -f "$DROPIN_DIR/10-no-namespaces.conf"
  rmdir "$DROPIN_DIR" 2>/dev/null || true
else
  warn "mount namespaces are unavailable here (unprivileged container?); relaxing the systemd sandbox"
  install -d -m 0755 "$DROPIN_DIR"
  cat > "$DROPIN_DIR/10-no-namespaces.conf" <<'DROPIN'
# Written by setup.sh: this environment cannot create mount namespaces.
# The service still runs as the unprivileged 'trackstar' user without capabilities.
[Service]
PrivateTmp=false
PrivateDevices=false
ProtectSystem=false
ProtectHome=false
ProtectKernelTunables=false
ProtectKernelModules=false
ProtectControlGroups=false
ReadWritePaths=
DROPIN
fi

systemctl daemon-reload
systemctl enable --quiet trackstar

addr=$(get_env TRACKSTAR_ADDR)
port=${addr##*:}
health_url="http://127.0.0.1:$port/health"

if [ "$START" -eq 0 ]; then
  log "Installed. Start with: systemctl start trackstar"
  exit 0
fi

log "Starting trackstar"
systemctl restart trackstar

healthy=0
for _ in $(seq 1 30); do
  if curl -fsS --max-time 2 "$health_url" >/dev/null 2>&1; then healthy=1; break; fi
  sleep 1
done

echo
systemctl --no-pager --lines=0 status trackstar || true
echo
if [ "$healthy" -ne 1 ]; then
  journalctl -u trackstar --no-pager -n 30 || true
  if [ "$WAS_ACTIVE" -eq 1 ] && [ -x "$BIN_PATH.previous" ]; then
    warn "restoring the previous binary"
    mv -f "$BIN_PATH.previous" "$BIN_PATH"
    systemctl restart trackstar || true
  fi
  die "trackstar did not become healthy at $health_url"
fi

cat <<DONE
Trackstar $NEW_VERSION is running.

  URL:      $(get_env TRACKSTAR_PUBLIC_URL)
  Health:   $health_url
  Config:   $ENV_FILE
  Data:     $DATA_DIR
  Logs:     journalctl -u trackstar -f
  Backup:   $OPT_DIR/scripts/backup.sh
  Update:   $OPT_DIR/scripts/update.sh

Open the URL and register: the first account becomes the administrator.
Afterwards set TRACKSTAR_ALLOW_REGISTRATION=false in $ENV_FILE to close sign-ups.
DONE
