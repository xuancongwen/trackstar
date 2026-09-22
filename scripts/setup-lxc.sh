#!/usr/bin/env bash
# Install Tracker inside an existing Debian/Ubuntu LXC (Proxmox or otherwise).
#
# Run this *inside* the container, as root, after you have created it, given it
# a network and updated it. It checks the container for what Tracker needs,
# installs the few required packages and then runs the normal scripts/setup.sh
# with any options you pass through:
#
#   ./setup-lxc.sh --public-url https://track.example.com/ --port 3000 --timezone Europe/Berlin
#
# The binary comes from the extracted release this script ships in, or from
# --binary PATH | --release-url URL | --repo OWNER/NAME [--version TAG]
# (all forwarded to setup.sh). Recommended container: unprivileged, Debian 13,
# 1 core, 512 MB RAM, 512 MB swap, 8 GB disk, no nesting required.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
FORCE=0
SKIP_UPGRADE=0
PASSTHRU=()

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
  cat <<'USAGE'

Options handled here:
  --force          continue even if this does not look like an LXC
  --skip-upgrade   do not run apt-get upgrade (you already did)
  -h, --help
Every other option is passed to setup.sh (see: scripts/setup.sh --help).
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --force) FORCE=1; shift ;;
    --skip-upgrade) SKIP_UPGRADE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) PASSTHRU+=("$1"); shift ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root inside the container"
[ -x "$SCRIPT_DIR/setup.sh" ] || die "scripts/setup.sh not found next to this script; run from an extracted release"

# --- 1. are we inside an LXC? --------------------------------------------------

virt=$(systemd-detect-virt --container 2>/dev/null || true)
case "$virt" in
  lxc|lxc-libvirt) ;;
  none|"")
    [ "$FORCE" -eq 1 ] || die "this does not look like a container (systemd-detect-virt: ${virt:-unknown}); use scripts/setup.sh on a plain VM, or --force" ;;
  *)
    warn "container type is '$virt', not lxc; continuing" ;;
esac

# shellcheck disable=SC1091
. /etc/os-release
case " ${ID:-} ${ID_LIKE:-} " in
  *" debian "*|*" ubuntu "*) ;;
  *) die "unsupported distribution '${PRETTY_NAME:-unknown}'; only Debian and Ubuntu are supported" ;;
esac

# --- 2. describe the container and warn about anything that matters -------------

read_cgroup() { # file → value or "max"
  local f="/sys/fs/cgroup/$1"
  [ -r "$f" ] && cat "$f" 2>/dev/null || echo max
}
human() { # bytes or "max"
  case "$1" in
    max|"") echo "unlimited" ;;
    *) awk -v b="$1" 'BEGIN { if (b >= 1073741824) printf "%.1f GB", b/1073741824; else printf "%d MB", b/1048576 }' ;;
  esac
}

mem_max=$(read_cgroup memory.max)
swap_max=$(read_cgroup memory.swap.max)
cores=$(nproc)
if grep -q '^ *0 *0 *4294967295' /proc/self/uid_map 2>/dev/null; then
  privileged="privileged"
else
  privileged="unprivileged"
fi
if unshare --mount true 2>/dev/null; then
  nesting="mount namespaces available (nesting on or privileged): full systemd sandbox"
else
  nesting="no mount namespaces (nesting off): setup.sh will install the relaxed-sandbox drop-in"
fi
ip_addr=$(hostname -I 2>/dev/null | awk '{print $1}') || true

log "Container: ${PRETTY_NAME}, ${privileged}, ${cores} core(s), RAM $(human "$mem_max"), swap $(human "$swap_max")"
log "Sandbox:   $nesting"

if [ "$mem_max" != max ] && [ "$mem_max" -lt $((200 * 1024 * 1024)) ]; then
  warn "less than 200 MB of RAM: Tracker itself needs ~50 MB, but apt upgrades may fail; 256–512 MB is recommended"
fi
if [ "$privileged" = privileged ]; then
  warn "privileged container: Tracker does not need it; an unprivileged container is the safer default"
fi
[ -n "$ip_addr" ] || warn "no IPv4 address on this container yet; the printed URL will be a placeholder"
if ! getent hosts deb.debian.org >/dev/null 2>&1 && ! getent hosts archive.ubuntu.com >/dev/null 2>&1; then
  warn "DNS resolution failed; package installation and downloads will not work"
fi

# --- 3. minimal packages -----------------------------------------------------------

export DEBIAN_FRONTEND=noninteractive
log "Updating package index"
apt-get update -qq
if [ "$SKIP_UPGRADE" -eq 0 ]; then
  log "Upgrading packages (--skip-upgrade to skip)"
  apt-get upgrade -y -qq >/dev/null
fi
# setup.sh installs what it needs (ca-certificates curl tar); this is only what
# a bare template may lack for the checks above and for day-to-day operation.
apt-get install -y -qq --no-install-recommends ca-certificates curl tar util-linux procps >/dev/null

# A persistent journal so `journalctl -u tracker` survives a container restart.
if [ ! -d /var/log/journal ]; then
  mkdir -p /var/log/journal
  systemd-tmpfiles --create --prefix /var/log/journal >/dev/null 2>&1 || true
  systemctl restart systemd-journald 2>/dev/null || true
fi

# --- 4. the normal installation ------------------------------------------------------

log "Running setup.sh ${PASSTHRU[*]+"${PASSTHRU[*]}"}"
bash "$SCRIPT_DIR/setup.sh" "${PASSTHRU[@]+"${PASSTHRU[@]}"}"

# --- 5. container-specific status ----------------------------------------------------

env_file=/etc/tracker/tracker.env
get_env() { sed -n "s/^$1=//p" "$env_file" 2>/dev/null | tail -n1; }
addr=$(get_env TRACKER_ADDR); port=${addr##*:}
status=$(systemctl is-active tracker 2>/dev/null || true)
dropin=/etc/systemd/system/tracker.service.d/10-no-namespaces.conf

cat <<DONE

Tracker is installed in this container.

  Hostname:     $(hostname)
  IP address:   ${ip_addr:-<none yet>}
  Local URL:    http://${ip_addr:-<container-ip>}:${port:-3000}/
  Public URL:   $(get_env TRACKER_PUBLIC_URL)
  Service:      ${status:-unknown}$([ -f "$dropin" ] && echo "  (relaxed systemd sandbox: $dropin)")
  Memory now:   $(awk '/MemTotal/ {t=$2} /MemAvailable/ {a=$2} END {printf "%d MB used of %d MB", (t-a)/1024, t/1024}' /proc/meminfo)

Next steps:
  * point your reverse proxy / Cloudflare Tunnel at http://${ip_addr:-<container-ip>}:${port:-3000}
    (see /opt/tracker/deploy/cloudflared-example.md)
  * open the public URL and register; the first account becomes the administrator
  * then set TRACKER_ALLOW_REGISTRATION=false in $env_file and: systemctl restart tracker
  * later: /opt/tracker/scripts/update.sh, backup.sh, restore.sh
DONE
[ "$status" = active ] || die "the tracker service is not active; see: journalctl -u tracker -n 50"
