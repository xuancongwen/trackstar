#!/usr/bin/env bash
# Set up a fresh Debian/Ubuntu machine for Trackstar and install it: an LXC
# (Proxmox or otherwise), a VM or a droplet. Run as root on the machine, or let
# the deploy do it: deploy/deploy.sh runs this script on its first contact.
# Safe to re-run: existing configuration, secrets and data are kept.
#
#   sudo ./deploy/setup.sh --public-url https://track.example.com/ --timezone Europe/Berlin
#
# What it does:
#   * reports what the machine provides (container?, cores, RAM, swap, free
#     disk) and warns about anything that would bite later
#   * installs the base packages a slim template may lack, enables SSH and
#     makes the journal persistent; a first install also runs apt-get upgrade
#   * optionally sets the time zone and authorizes an SSH public key for root
#   * installs the application: trackstar user, /etc/trackstar,
#     /var/lib/trackstar, the binary and the hardened systemd unit (relaxed
#     only where a container cannot sandbox), then waits for /health
#
# The binary comes from (first match wins):
#   --binary PATH          a trackstar executable
#   ../trackstar             when run from an extracted release archive
#   ../bin/trackstar         when run from a source checkout after `make build`
#   --release-url URL      a release .tar.gz to download
#   --repo OWNER/NAME      the latest (or --version TAG) GitHub release
# With none of these and nothing installed yet, only the system is prepared,
# ready for a deploy from your workstation.
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
AUTHORIZED_KEY=""
SKIP_UPGRADE=0
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

usage() { sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'; cat <<'USAGE'

Options:
  --public-url URL            external URL (default http://<this-ip>:<port>/)
  --port N                    listen port (default 3000)
  --bind HOST                 listen host (default 0.0.0.0)
  --timezone ZONE             IANA zone for the system clock and for iteration
                              boundaries (default: system unchanged, iterations in UTC)
  --allow-registration BOOL   true|false (default true)
  --authorized-key ARG        public key file or literal "ssh-ed25519 …" to append to
                              /root/.ssh/authorized_keys so deploy.sh can connect
  --skip-upgrade              do not run apt-get upgrade on a first install
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
    --authorized-key) AUTHORIZED_KEY=${2:?}; shift 2 ;;
    --skip-upgrade) SKIP_UPGRADE=1; shift ;;
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

# --- the machine: describe it, warn about anything that matters later ------------

virt=$(systemd-detect-virt 2>/dev/null || true)
container=$(systemd-detect-virt --container 2>/dev/null || true)
case "$container" in
  none|"") kind=${virt:-none}; [ "$kind" != none ] || kind="bare metal" ;;
  *)
    # A full uid map means the container's root is the host's root.
    if grep -q '^ *0 *0 *4294967295' /proc/self/uid_map 2>/dev/null; then
      kind="$container container, privileged"
    else
      kind="$container container, unprivileged"
    fi ;;
esac
# In an LXC /proc/meminfo shows the container's limits, not the host's.
mem_mb=$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo)
swap_mb=$(awk '/^SwapTotal:/ {print int($2/1024)}' /proc/meminfo)
disk_free=$(df -h --output=avail / 2>/dev/null | tail -n1 | tr -d ' ')
ip_addr=$(hostname -I 2>/dev/null | awk '{print $1}') || true

log "Machine: ${PRETTY_NAME} (${ARCH}), ${kind}, $(nproc) core(s), ${mem_mb} MB RAM, ${swap_mb} MB swap, ${disk_free:-?} free on /"
if [ "$mem_mb" -lt 200 ]; then
  warn "under 200 MB of RAM: Trackstar itself needs ~50 MB, but apt upgrades may fail; 256–512 MB is recommended"
fi
case "$kind" in
  *", privileged") warn "privileged container: Trackstar does not need it; unprivileged is the safer default" ;;
esac
[ -n "$ip_addr" ] || warn "no IPv4 address yet; a deploy needs to reach this machine over SSH"

# --- packages -------------------------------------------------------------------

# A machine without configuration has not been set up before: bring it up to
# date once. Re-runs leave the system's packages alone.
upgrade=0
[ -f "$ENV_FILE" ] || [ "$SKIP_UPGRADE" -eq 1 ] || upgrade=1

# What this script and the operational scripts rely on, sshd for the deploy,
# and the usual troubleshooting tools a slim template leaves out.
missing=()
for pkg in ca-certificates curl tar tzdata util-linux procps openssh-server less nano; do
  dpkg -s "$pkg" >/dev/null 2>&1 || missing+=("$pkg")
done
if [ "$upgrade" -eq 1 ] || [ ${#missing[@]} -gt 0 ]; then
  if ! getent hosts deb.debian.org >/dev/null 2>&1 && ! getent hosts archive.ubuntu.com >/dev/null 2>&1; then
    die "DNS resolution failed; fix networking first (check /etc/resolv.conf)"
  fi
  export DEBIAN_FRONTEND=noninteractive
  log "Updating package index"
  apt-get update -qq
  if [ "$upgrade" -eq 1 ]; then
    log "Upgrading packages (first install; --skip-upgrade to skip)"
    apt-get upgrade -y -qq >/dev/null
  fi
  if [ ${#missing[@]} -gt 0 ]; then
    log "Installing packages: ${missing[*]}"
    apt-get install -y -qq --no-install-recommends "${missing[@]}" >/dev/null
  fi
fi
systemctl enable --now ssh >/dev/null 2>&1 || systemctl enable --now sshd >/dev/null 2>&1 || warn "could not enable the SSH service"

# --- system settings ------------------------------------------------------------

if [ ! -d /var/log/journal ]; then
  log "Making the journal persistent"
  mkdir -p /var/log/journal
  systemd-tmpfiles --create --prefix /var/log/journal >/dev/null 2>&1 || true
  systemctl restart systemd-journald 2>/dev/null || true
fi

if [ -n "$TIMEZONE" ]; then
  [ -f "/usr/share/zoneinfo/$TIMEZONE" ] || die "unknown time zone '$TIMEZONE'"
  log "Setting system time zone to $TIMEZONE"
  if command -v timedatectl >/dev/null && timedatectl set-timezone "$TIMEZONE" 2>/dev/null; then :; else
    ln -sf "/usr/share/zoneinfo/$TIMEZONE" /etc/localtime
    echo "$TIMEZONE" > /etc/timezone
  fi
fi

if [ -n "$AUTHORIZED_KEY" ]; then
  if [ -f "$AUTHORIZED_KEY" ]; then
    key=$(grep -v '^#' "$AUTHORIZED_KEY" | grep -m1 . || true)
  else
    key=$AUTHORIZED_KEY
  fi
  case "$key" in
    ssh-*|ecdsa-*|sk-*) ;;
    *) die "--authorized-key: not an SSH public key" ;;
  esac
  install -d -m 0700 /root/.ssh
  touch /root/.ssh/authorized_keys
  chmod 0600 /root/.ssh/authorized_keys
  if grep -qxF "$key" /root/.ssh/authorized_keys; then
    log "SSH key already authorized for root"
  else
    echo "$key" >> /root/.ssh/authorized_keys
    log "Authorized SSH key for root"
  fi
  if sshd -T 2>/dev/null | grep -qi '^permitrootlogin no'; then
    warn "sshd has PermitRootLogin no; set 'PermitRootLogin prohibit-password' in /etc/ssh/sshd_config and restart ssh"
  fi
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
  [ -f "$WORK/release/deploy/trackstar.service" ] && ROOT_DIR=$WORK/release
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
    cat <<DONE

The system is prepared, but there was no trackstar binary to install.

Deploy from your workstation (installs now, swaps the binary afterwards):

  ./deploy/deploy.sh root@${ip_addr:-<this-machine>} -- --public-url https://track.example.com/

or re-run this script with --repo OWNER/NAME, --release-url URL or --binary PATH.
DONE
    exit 0
  fi
fi
[ -f "$BINARY" ] || die "binary not found: $BINARY"
[ -f "$ROOT_DIR/deploy/trackstar.service" ] \
  || die "trackstar.service not found beside this script; run it from a release archive or a checkout, or pass --repo / --release-url"
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

# Re-run from its installed location, the script has nothing to copy onto itself.
if [ "$ROOT_DIR" != "$OPT_DIR" ]; then
  for f in "$ROOT_DIR"/scripts/*.sh; do
    [ -f "$f" ] && install -m 0755 "$f" "$OPT_DIR/scripts/"
  done
  for f in "$ROOT_DIR"/deploy/*; do
    [ -f "$f" ] || continue
    case "${f##*/}" in
      deploy.sh|deploy.env) ;; # workstation side only
      *.sh) install -m 0755 "$f" "$OPT_DIR/deploy/" ;;
      *) install -m 0644 "$f" "$OPT_DIR/deploy/" ;;
    esac
  done
fi
# These lived in scripts/ before they moved to deploy/.
rm -f "$OPT_DIR/scripts/setup.sh" "$OPT_DIR/scripts/setup-lxc.sh" "$OPT_DIR/scripts/deploy.sh"
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
  umask 027
  cat > "$ENV_FILE" <<ENV
# Trackstar configuration. Documented in $OPT_DIR/deploy/trackstar.env.example
TRACKSTAR_ADDR=${BIND_HOST:-0.0.0.0}:$port
TRACKSTAR_DATA_DIR=$DATA_DIR
TRACKSTAR_DATABASE_DRIVER=sqlite
TRACKSTAR_DATABASE_URL=$DATA_DIR/trackstar.db
TRACKSTAR_PUBLIC_URL=${PUBLIC_URL:-http://${ip_addr:-localhost}:$port/}
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
  Update:   $OPT_DIR/scripts/update.sh, or ./deploy/deploy.sh root@${ip_addr:-<this-machine>} from a checkout

Open the URL and register: the first account becomes the administrator.
Afterwards set TRACKSTAR_ALLOW_REGISTRATION=false in $ENV_FILE to close sign-ups.
DONE
