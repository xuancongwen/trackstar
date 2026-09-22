#!/usr/bin/env bash
# Prepare a Debian/Ubuntu LXC (Proxmox or otherwise) at the system level so
# Trackstar can be deployed into it. Run *inside* the container, as root, once.
#
#   ./setup-lxc.sh [--timezone Europe/Berlin] [--authorized-key ~/.ssh/id_ed25519.pub] [--skip-upgrade]
#
# What it does — and deliberately nothing application-specific:
#   * checks that this is an LXC on Debian/Ubuntu and reports its resources
#     (privileged?, cores, RAM/swap limits, mount-namespace support) with
#     warnings for anything that would bite later
#   * apt update / upgrade and the base packages a template may lack
#   * persistent systemd journal
#   * optionally: system timezone, an SSH public key for root
# The application (binary, trackstar user, /etc/trackstar, /var/lib/trackstar,
# systemd unit) is installed by the deploy:
#   ./scripts/deploy.sh root@<container-ip> -- --public-url https://track.example.com/
# which runs scripts/setup.sh on the first contact and only swaps the binary
# afterwards. The script is standalone; copy just this file into the container.
set -euo pipefail

FORCE=0
SKIP_UPGRADE=0
TIMEZONE=""
AUTHORIZED_KEY=""

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  cat <<'USAGE'

Options:
  --timezone ZONE         set the container's system time zone (journal timestamps; the
                          application's iteration zone is TRACKSTAR_TIMEZONE, set at deploy)
  --authorized-key ARG    public key file or literal "ssh-ed25519 …" to append to
                          /root/.ssh/authorized_keys so deploy.sh can connect
  --skip-upgrade          do not run apt-get upgrade
  --force                 continue even if this does not look like an LXC
  -h, --help
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --timezone) TIMEZONE=${2:?}; shift 2 ;;
    --authorized-key) AUTHORIZED_KEY=${2:?}; shift 2 ;;
    --skip-upgrade) SKIP_UPGRADE=1; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help). Application options belong to the deploy: deploy.sh … -- --public-url …" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root inside the container"

# --- 1. are we inside an LXC on a supported distribution? -------------------------

virt=$(systemd-detect-virt --container 2>/dev/null || true)
case "$virt" in
  lxc|lxc-libvirt) ;;
  none|"")
    [ "$FORCE" -eq 1 ] || die "this does not look like a container (systemd-detect-virt: ${virt:-unknown}); a plain VM needs no preparation beyond the deploy, or use --force" ;;
  *) warn "container type is '$virt', not lxc; continuing" ;;
esac

[ -r /etc/os-release ] || die "/etc/os-release not found"
# shellcheck disable=SC1091
. /etc/os-release
case " ${ID:-} ${ID_LIKE:-} " in
  *" debian "*|*" ubuntu "*) ;;
  *) die "unsupported distribution '${PRETTY_NAME:-unknown}'; only Debian and Ubuntu are supported" ;;
esac
command -v systemctl >/dev/null || die "systemd is required (is this a systemd-based template?)"

# --- 2. describe the container; warn about anything that matters later ------------

read_cgroup() { local f="/sys/fs/cgroup/$1"; { [ -r "$f" ] && cat "$f"; } 2>/dev/null || echo max; }
human() {
  case "$1" in
    max|"") echo unlimited ;;
    *) awk -v b="$1" 'BEGIN { if (b >= 1073741824) printf "%.1f GB", b/1073741824; else printf "%d MB", b/1048576 }' ;;
  esac
}

mem_max=$(read_cgroup memory.max)
swap_max=$(read_cgroup memory.swap.max)
cores=$(nproc)
privileged=unprivileged
grep -q '^ *0 *0 *4294967295' /proc/self/uid_map 2>/dev/null && privileged=privileged
if unshare --mount true 2>/dev/null; then
  namespaces="available (nesting on, or privileged) → full systemd sandbox"
else
  namespaces="unavailable (nesting off) → the deploy installs a relaxed-sandbox drop-in; the service still runs unprivileged"
fi
disk_free=$(df -h --output=avail / 2>/dev/null | tail -n1 | tr -d ' ')
ip_addr=$(hostname -I 2>/dev/null | awk '{print $1}') || true

log "Container:        ${PRETTY_NAME}, ${privileged}, ${cores} core(s), RAM $(human "$mem_max"), swap $(human "$swap_max"), ${disk_free:-?} free on /"
log "Mount namespaces: $namespaces"

if [ "$mem_max" != max ] && [ "$mem_max" -lt $((200 * 1024 * 1024)) ]; then
  warn "under 200 MB of RAM: Trackstar itself needs ~50 MB, but apt upgrades may fail; 256–512 MB is recommended"
fi
[ "$privileged" = unprivileged ] || warn "privileged container: Trackstar does not need it; unprivileged is the safer default"
[ -n "$ip_addr" ] || warn "no IPv4 address yet; the deploy needs to reach this container over SSH"
if ! getent hosts deb.debian.org >/dev/null 2>&1 && ! getent hosts archive.ubuntu.com >/dev/null 2>&1; then
  die "DNS resolution failed; fix networking first (check /etc/resolv.conf)"
fi

# --- 3. packages ------------------------------------------------------------------------

export DEBIAN_FRONTEND=noninteractive
log "Updating package index"
apt-get update -qq
if [ "$SKIP_UPGRADE" -eq 0 ]; then
  log "Upgrading packages (--skip-upgrade to skip)"
  apt-get upgrade -y -qq >/dev/null
fi
# Everything setup.sh and the operational scripts rely on, plus sshd for the
# deploy and the usual troubleshooting tools a slim template leaves out.
log "Installing base packages"
apt-get install -y -qq --no-install-recommends \
  ca-certificates curl tar openssh-server util-linux procps less nano >/dev/null
apt-get autoremove -y -qq >/dev/null
systemctl enable --now ssh >/dev/null 2>&1 || systemctl enable --now sshd >/dev/null 2>&1 || warn "could not enable the SSH service"

# --- 4. system settings -------------------------------------------------------------------

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

# --- 5. report ----------------------------------------------------------------------------

cat <<DONE

Container is ready for Trackstar.

  Hostname:     $(hostname)
  IP address:   ${ip_addr:-<none yet>}
  SSH:          $(systemctl is-active ssh 2>/dev/null || systemctl is-active sshd 2>/dev/null || echo unknown)
  Memory now:   $(awk '/MemTotal/ {t=$2} /MemAvailable/ {a=$2} END {printf "%d MB used of %d MB", (t-a)/1024, t/1024}' /proc/meminfo)

Deploy from your workstation (installs on first run, swaps the binary afterwards):

  ./scripts/deploy.sh root@${ip_addr:-<container-ip>} -- --public-url https://track.example.com/ --timezone ${TIMEZONE:-UTC}

Nothing application-specific was installed here; setup.sh (run by the deploy)
creates the trackstar user, /etc/trackstar, /var/lib/trackstar and the service.
DONE
