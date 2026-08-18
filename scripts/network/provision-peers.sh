#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Provision a freshly cloned "peers" UTM VM's guest OS for the network harness:
#   1. de-conflict its DHCP identity and stop the dhcpcd<->macvlan ARP churn,
#   2. wait for a stable bridged LAN IP,
#   3. install the SSH key and verify it with a real login,
#   4. grant passwordless sudo and verify it with `sudo -n` over that login,
#   5. pin the ARP sysctls the macvlan children need.
#
# Prints the resolved peers LAN IP as the LAST line of STDOUT; all progress goes
# to STDERR, so the caller can capture the IP with `$(... )`.
#
# This lives in a script (not the taskfile) because the guest-agent interaction is
# finicky: `utmctl exec` returns 0 and does not forward guest stdout even when an
# early write silently no-ops, so we cannot trust exit codes — we VERIFY by effect
# (a stable IP, then a real SSH login). Running it in real bash also avoids the
# `set -e` foot-guns of go-task's mvdan/sh interpreter.
#
# Usage:
#   provision-peers.sh <vm-name> <ssh-priv-key> <ssh-pub-key> <vm-user> <roles> [ssh-opts]
#     <roles>    space-separated macvlan role names (e.g. "publisher partner ...");
#                each becomes a "<role>0" device that dhcpcd must ignore.
#     [ssh-opts] extra ssh flags (word-split); e.g. "-o StrictHostKeyChecking=no ..."
set -euo pipefail

VM="${1:?vm-name}"; PRIV="${2:?ssh-priv-key}"; PUB="${3:?ssh-pub-key}"
USER_="${4:?vm-user}"; ROLES="${5:?roles}"; SSH_OPTS="${6:-}"

HERE="$(cd "$(dirname "$0")" && pwd)"
log() { echo "$@" >&2; }
peers_ip() { "$HERE/peers-ip.sh" "$VM"; }

# Retry a guest command over the UTM guest agent. qemu-guest-agent throws a
# transient OSStatus -2700 for the first few seconds after boot; retry through it.
gexec() {
  local n=0
  while [ "$n" -lt 8 ]; do
    if utmctl exec "$VM" --cmd /bin/bash -c "$1" 2>/dev/null; then return 0; fi
    n=$((n + 1)); sleep 2
  done
  return 1
}

# Wait until the guest agent answers at all (bounded). We do NOT trust it beyond
# this single probe — the later steps verify by effect — but the DHCP step needs a
# live agent to act on.
log "Waiting for the UTM guest agent..."
for _ in $(seq 1 60); do
  if utmctl exec "$VM" --cmd /usr/bin/id >/dev/null 2>&1; then break; fi
  sleep 2
done

# 1. De-conflict DHCP and stop the dhcpcd<->macvlan ARP churn (idempotent, guarded
#    inside the guest shell). A clone inherits the golden's persistent dhcpcd DUID,
#    so the peers and BN VMs present the SAME DHCP identity; and dhcpcd's ARP
#    conflict-detection on the macvlan parent makes it decline and re-request leases
#    endlessly (the IP "flips" through the pool). Fix: identify by the now-unique MAC
#    via `clientid`, drop the inherited DUID, add `noarp`, and ignore the macvlan
#    children (derived from the role names so it stays in sync with the caller).
DENY=""
for r in $ROLES; do DENY="$DENY ${r}0"; done
DENY="${DENY# }"
log "Applying DHCP de-confliction (idempotent; denyinterfaces: $DENY)..."
DECONF='if ! grep -q "^clientid" /etc/dhcpcd.conf; then '
DECONF+='sed -i "s/^duid$/clientid/" /etc/dhcpcd.conf; '
DECONF+='grep -q "^noarp$" /etc/dhcpcd.conf || echo noarp >> /etc/dhcpcd.conf; '
DECONF+="grep -q '^denyinterfaces ' /etc/dhcpcd.conf || echo 'denyinterfaces $DENY' >> /etc/dhcpcd.conf; "
DECONF+='rm -f /var/lib/dhcpcd/duid; dhcpcd -k enp0s1 2>/dev/null; sleep 1; dhcpcd -b enp0s1 2>/dev/null; fi'
gexec "$DECONF" || true

# 2. Wait for a STABLE primary LAN IP: two consecutive equal reads, so we do not
#    latch onto a lease that is still settling right after the dhcpcd restart.
log "Waiting for a stable peers VM LAN IP..."
PEERS_IP=""; last=""
for _ in $(seq 1 30); do
  cur="$(peers_ip)"
  if [ -n "$cur" ] && [ "$cur" = "$last" ]; then PEERS_IP="$cur"; break; fi
  last="$cur"; sleep 5
done
if [ -z "$PEERS_IP" ]; then
  PEERS_IP="$(peers_ip)"
  [ -n "$PEERS_IP" ] && log "⚠️  peers LAN IP did not stabilize; proceeding with $PEERS_IP (verify it is not flipping)"
fi
if [ -z "$PEERS_IP" ]; then log "❌ peers VM never got an IP"; exit 1; fi
log "✓ peers VM at $PEERS_IP"

# 3. Install the SSH key and VERIFY it by an actual login. The guest agent reports
#    success even when an early write did not persist, so re-install and re-test in a
#    loop until `ssh` truly connects. BatchMode=yes keeps a failed attempt from
#    blocking on a password prompt.
log "Installing SSH key and verifying login..."
PUBKEY="$(cat "$PUB")"
SSH_OK=""
for _ in $(seq 1 15); do
  gexec "mkdir -p /home/$USER_/.ssh && chmod 700 /home/$USER_/.ssh" || true
  gexec "printf '%s\n' \"$PUBKEY\" > /home/$USER_/.ssh/authorized_keys && chmod 600 /home/$USER_/.ssh/authorized_keys && chown -R $USER_:$USER_ /home/$USER_/.ssh" || true
  gexec "systemctl start ssh 2>/dev/null || systemctl start sshd 2>/dev/null || true" || true
  if ssh -i "$PRIV" $SSH_OPTS -o BatchMode=yes -o ConnectTimeout=5 "$USER_@$PEERS_IP" true 2>/dev/null; then
    SSH_OK=1; break
  fi
  sleep 5
done
if [ -z "$SSH_OK" ]; then log "❌ SSH key never took effect on the peers VM (login still failing)"; exit 1; fi
log "✓ SSH key working on $PEERS_IP"

# 4. Grant passwordless sudo, then VERIFY with `sudo -n` over a real SSH login.
#    The caller sets up the macvlan children over non-interactive SSH, where a
#    sudo password prompt cannot be answered: every `sudo` fails, and because
#    those failures are non-fatal the harness goes on to report peer IPs that
#    were never created. The golden image carries no such drop-in — vm.yaml
#    installs one only on the BN VM, which is why that VM works and this one
#    does not.
log "Configuring passwordless sudo..."
SUDO_OK=""
for _ in $(seq 1 5); do
  gexec "echo '$USER_ ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/$USER_ && chmod 440 /etc/sudoers.d/$USER_" || true
  if ssh -i "$PRIV" $SSH_OPTS -o BatchMode=yes -o ConnectTimeout=5 "$USER_@$PEERS_IP" "sudo -n true" 2>/dev/null; then
    SUDO_OK=1; break
  fi
  sleep 2
done
if [ -z "$SUDO_OK" ]; then log "❌ passwordless sudo never took effect on the peers VM"; exit 1; fi
log "✓ passwordless sudo working on $PEERS_IP"

# 5. Stop ARP flux before any macvlan child exists. All six addresses share one
#    /24, and with the default arp_ignore=0 every interface answers ARP for every
#    local address — the upstream switch can then bind the parent's management IP
#    to a child's MAC and the peers VM drops off mid-run. arp_announce=2 keeps the
#    parent from sourcing ARP with a child's address. Written to /etc/sysctl.d so
#    it survives the VM restarts this harness does between runs.
log "Hardening ARP for the multi-homed peers VM..."
gexec "printf 'net.ipv4.conf.all.arp_ignore=1\nnet.ipv4.conf.all.arp_announce=2\n' > /etc/sysctl.d/99-solo-weaver-peers.conf && sysctl -p /etc/sysctl.d/99-solo-weaver-peers.conf >/dev/null" || true

# Emit the resolved IP as the last stdout line for the caller to capture.
echo "$PEERS_IP"
