#!/usr/bin/env bash
# Create a dedicated unprivileged Debian LXC for Tracker on a Proxmox VE host
# and install Tracker inside it with the normal setup.sh.
#
#   ./setup-lxc.sh --vmid 120 --hostname tracker --storage local-lvm --bridge vmbr0 \
#                  --memory 512 --cores 1 --disk 8 --ip dhcp
#   ./setup-lxc.sh --vmid 120 --ip 192.168.1.240/24 --gateway 192.168.1.1 \
#                  --public-url https://track.example.com/
#
# Run it from an extracted release archive (it ships ./tracker next to scripts/),
# or point it at a release with --release PATH.tar.gz | --release-url URL.
# No Docker, no nesting, no privileged container.
set -euo pipefail

VMID=""
CT_HOSTNAME=tracker
STORAGE=local-lvm
TEMPLATE_STORAGE=local
TEMPLATE=""
BRIDGE=vmbr0
MEMORY=512
SWAP=512
CORES=1
DISK=8
IP=dhcp
GATEWAY=""
NAMESERVER=""
VLAN=""
PORT=3000
PUBLIC_URL=""
TIMEZONE=""
RELEASE=""
RELEASE_URL=""
SSH_KEYS=""
NESTING=0
ONBOOT=1

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; cat <<'USAGE'

Container:
  --vmid N               container id (default: next free id)
  --hostname NAME        (default tracker)
  --storage NAME         rootfs storage (default local-lvm)
  --template-storage N   storage holding CT templates (default local)
  --template VOLID       use this template instead of the newest Debian standard one
  --bridge NAME          (default vmbr0)      --vlan TAG
  --memory MB            (default 512)        --swap MB   (default 512)
  --cores N              (default 1)          --disk GB   (default 8)
  --ip dhcp|CIDR         (default dhcp)       --gateway IP   --nameserver IP
  --ssh-keys FILE        authorized_keys for root inside the container
  --no-onboot            do not start the container at host boot
  --nesting              enable LXC nesting (not needed; setup.sh adapts the
                         systemd sandbox instead — see README)
Tracker:
  --port N               (default 3000)
  --public-url URL       (default http://<container-ip>:<port>/)
  --timezone ZONE        IANA zone for iteration boundaries
  --release FILE         release .tar.gz to install
  --release-url URL      release .tar.gz to download inside the container
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --vmid) VMID=${2:?}; shift 2 ;;
    --hostname) CT_HOSTNAME=${2:?}; shift 2 ;;
    --storage) STORAGE=${2:?}; shift 2 ;;
    --template-storage) TEMPLATE_STORAGE=${2:?}; shift 2 ;;
    --template) TEMPLATE=${2:?}; shift 2 ;;
    --bridge) BRIDGE=${2:?}; shift 2 ;;
    --vlan) VLAN=${2:?}; shift 2 ;;
    --memory) MEMORY=${2:?}; shift 2 ;;
    --swap) SWAP=${2:?}; shift 2 ;;
    --cores) CORES=${2:?}; shift 2 ;;
    --disk) DISK=${2:?}; shift 2 ;;
    --ip) IP=${2:?}; shift 2 ;;
    --gateway) GATEWAY=${2:?}; shift 2 ;;
    --nameserver) NAMESERVER=${2:?}; shift 2 ;;
    --ssh-keys) SSH_KEYS=${2:?}; shift 2 ;;
    --no-onboot) ONBOOT=0; shift ;;
    --nesting) NESTING=1; shift ;;
    --port) PORT=${2:?}; shift 2 ;;
    --public-url) PUBLIC_URL=${2:?}; shift 2 ;;
    --timezone) TIMEZONE=${2:?}; shift 2 ;;
    --release) RELEASE=${2:?}; shift 2 ;;
    --release-url) RELEASE_URL=${2:?}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

# --- 1. validate the host -------------------------------------------------------

[ "$(id -u)" -eq 0 ] || die "run as root on the Proxmox host"
for tool in pct pvesh pveam pvesm; do
  command -v "$tool" >/dev/null || die "'$tool' not found: this must run on a Proxmox VE host"
done
[ -d /etc/pve ] || die "/etc/pve not found: this must run on a Proxmox VE host"

for n in MEMORY SWAP CORES DISK PORT; do
  case "${!n}" in ''|*[!0-9]*) die "--$(echo "$n" | tr '[:upper:]' '[:lower:]') must be a number" ;; esac
done
if [ "$IP" != dhcp ]; then
  case "$IP" in */*) ;; *) die "--ip must be 'dhcp' or CIDR notation such as 192.168.1.240/24" ;; esac
  [ -n "$GATEWAY" ] || die "--gateway is required with a static --ip"
elif [ -n "$GATEWAY" ]; then
  die "--gateway only makes sense with a static --ip"
fi
[ -z "$SSH_KEYS" ] || [ -f "$SSH_KEYS" ] || die "ssh key file not found: $SSH_KEYS"

if [ -z "$VMID" ]; then
  VMID=$(pvesh get /cluster/nextid)
  log "Using next free VMID $VMID"
fi
case "$VMID" in *[!0-9]*) die "--vmid must be a number" ;; esac
if pct status "$VMID" >/dev/null 2>&1 || qm status "$VMID" >/dev/null 2>&1; then
  die "VMID $VMID is already in use"
fi
pvesm status --storage "$STORAGE" >/dev/null 2>&1 || die "storage '$STORAGE' not found (see: pvesm status)"
pvesm status --storage "$TEMPLATE_STORAGE" >/dev/null 2>&1 || die "template storage '$TEMPLATE_STORAGE' not found"
[ -d "/sys/class/net/$BRIDGE" ] || die "bridge '$BRIDGE' does not exist on this host"

# --- locate the Tracker release before creating anything -------------------------

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(dirname "$SCRIPT_DIR")
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

PAYLOAD=""
if [ -n "$RELEASE" ]; then
  [ -f "$RELEASE" ] || die "release not found: $RELEASE"
  PAYLOAD=$RELEASE
elif [ -z "$RELEASE_URL" ]; then
  bin=""
  [ -x "$ROOT_DIR/tracker" ] && bin=$ROOT_DIR/tracker
  [ -z "$bin" ] && [ -x "$ROOT_DIR/bin/tracker" ] && bin=$ROOT_DIR/bin/tracker
  [ -n "$bin" ] || die "no Tracker release found: run from an extracted release, or pass --release / --release-url"
  mkdir -p "$WORK/tracker-release"
  cp "$bin" "$WORK/tracker-release/tracker"
  cp -r "$ROOT_DIR/scripts" "$ROOT_DIR/deploy" "$WORK/tracker-release/"
  tar -C "$WORK" -czf "$WORK/tracker-release.tar.gz" tracker-release
  PAYLOAD=$WORK/tracker-release.tar.gz
fi

# --- 2. template ------------------------------------------------------------------

if [ -z "$TEMPLATE" ]; then
  log "Looking for a Debian template"
  pveam update >/dev/null 2>&1 || warn "pveam update failed; using the cached template index"
  tmpl_name=$(pveam available --section system | awk '{print $2}' | grep -E '^debian-1[0-9]-standard_.*_amd64\.tar\.(zst|gz|xz)$' | sort -V | tail -n1) || true
  [ -n "$tmpl_name" ] || die "no Debian standard template in the pveam index; pass --template"
  TEMPLATE=$TEMPLATE_STORAGE:vztmpl/$tmpl_name
  if ! pveam list "$TEMPLATE_STORAGE" | awk '{print $1}' | grep -qx "$TEMPLATE"; then
    log "Downloading $tmpl_name"
    pveam download "$TEMPLATE_STORAGE" "$tmpl_name" >/dev/null
  fi
fi
log "Template: $TEMPLATE"

# --- 3./4. create -----------------------------------------------------------------

net0="name=eth0,bridge=$BRIDGE,ip=$IP"
[ -z "$GATEWAY" ] || net0+=",gw=$GATEWAY"
[ -z "$VLAN" ] || net0+=",tag=$VLAN"
[ "$IP" != dhcp ] || net0+=",ip6=auto"

create_args=(
  "$VMID" "$TEMPLATE"
  --hostname "$CT_HOSTNAME"
  --ostype debian
  --unprivileged 1
  --cores "$CORES" --memory "$MEMORY" --swap "$SWAP"
  --rootfs "$STORAGE:$DISK"
  --net0 "$net0"
  --onboot "$ONBOOT"
  --description "Tracker project tracker - created by setup-lxc.sh"
)
[ -z "$NAMESERVER" ] || create_args+=(--nameserver "$NAMESERVER")
[ -z "$SSH_KEYS" ] || create_args+=(--ssh-public-keys "$SSH_KEYS")
[ "$NESTING" -eq 0 ] || create_args+=(--features nesting=1)

log "Creating container $VMID ($CORES core, ${MEMORY} MB RAM, ${SWAP} MB swap, ${DISK} GB on $STORAGE)"
pct create "${create_args[@]}" >/dev/null

# From here on, tell the user how to clean up if something fails.
on_error() { warn "setup failed. Inspect with 'pct enter $VMID' or remove with: pct stop $VMID; pct destroy $VMID"; }
trap 'on_error; rm -rf "$WORK"' ERR

# --- 5. start ---------------------------------------------------------------------

log "Starting container"
pct start "$VMID"

log "Waiting for network"
CT_IP=""
for _ in $(seq 1 60); do
  CT_IP=$(pct exec "$VMID" -- sh -c "ip -4 -o addr show dev eth0 scope global 2>/dev/null | awk '{print \$4}' | cut -d/ -f1 | head -n1") || true
  [ -n "$CT_IP" ] && break
  sleep 1
done
[ -n "$CT_IP" ] || die "container did not get an IPv4 address on eth0 (check bridge/DHCP/VLAN)"
log "Container IP: $CT_IP"

for _ in $(seq 1 30); do
  pct exec "$VMID" -- sh -c 'getent hosts deb.debian.org >/dev/null' 2>/dev/null && break
  sleep 1
done

# --- 6.–9. install inside the container ---------------------------------------------

setup_args=(--port "$PORT" --public-url "${PUBLIC_URL:-http://$CT_IP:$PORT/}")
[ -z "$TIMEZONE" ] || setup_args+=(--timezone "$TIMEZONE")

if [ -n "$PAYLOAD" ]; then
  log "Copying the Tracker release into the container"
  pct push "$VMID" "$PAYLOAD" /root/tracker-release.tar.gz
  pct exec "$VMID" -- sh -c 'rm -rf /root/tracker-release && mkdir -p /root/tracker-release && tar -xzf /root/tracker-release.tar.gz -C /root/tracker-release --strip-components=1 --no-same-owner'
else
  # Bootstrap: setup.sh downloads the release itself, it only needs curl first.
  log "Downloading the Tracker release inside the container"
  pct exec "$VMID" -- sh -c 'export DEBIAN_FRONTEND=noninteractive; apt-get update -qq && apt-get install -y -qq --no-install-recommends ca-certificates curl >/dev/null'
  pct exec "$VMID" -- sh -c "rm -rf /root/tracker-release && mkdir -p /root/tracker-release && curl -fsSL --retry 3 '$RELEASE_URL' | tar -xz -C /root/tracker-release --strip-components=1 --no-same-owner"
fi

log "Running setup.sh inside the container"
pct exec "$VMID" -- bash /root/tracker-release/scripts/setup.sh "${setup_args[@]}"
pct exec "$VMID" -- rm -rf /root/tracker-release /root/tracker-release.tar.gz

# --- 10. report -------------------------------------------------------------------

trap 'rm -rf "$WORK"' ERR
status=$(pct exec "$VMID" -- systemctl is-active tracker 2>/dev/null || true)
cat <<DONE

Tracker LXC is ready.

  Container ID:   $VMID ($CT_HOSTNAME)
  IP address:     $CT_IP
  Local URL:      http://$CT_IP:$PORT/
  Public URL:     ${PUBLIC_URL:-http://$CT_IP:$PORT/}
  Service:        ${status:-unknown}

  Shell:          pct enter $VMID
  Logs:           pct exec $VMID -- journalctl -u tracker -f
  Backup:         pct exec $VMID -- /opt/tracker/scripts/backup.sh
  Update:         pct exec $VMID -- /opt/tracker/scripts/update.sh --file <release.tar.gz>
DONE
[ "$IP" = dhcp ] && echo "  Note: the address comes from DHCP; add a reservation or re-create with --ip <cidr> --gateway <ip>."
[ "$status" = active ] || die "the tracker service is not active; see the logs above"
