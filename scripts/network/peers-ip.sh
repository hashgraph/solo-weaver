#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Print the bridged LAN IPv4 of a UTM VM, or nothing until DHCP assigns it.
#
# `utmctl ip-address` lists every guest interface in arbitrary order — the golden
# image runs Docker (docker0, compose bridges) and a provisioned node adds
# cilium_host inside the cluster pod CIDR. Rather than blocklisting known-bad
# ranges (a list that grows every time the guest gains a network), select by the
# property callers actually need: the guest address inside one of the HOST's own
# interface subnets is the one the host can SSH to. Guest-internal addresses
# never match (the host has no interface on those subnets), and while the real
# NIC is still waiting for its DHCP lease nothing matches — callers get an EMPTY
# result, so wait loops keep polling.
#
# This is the repo's single VM-IP resolver — don't re-inline selection logic at
# call sites.
#
# Usage: peers-ip.sh <vm-name>
set -euo pipefail

VM="${1:?usage: peers-ip.sh <vm-name>}"

# 192.168.0.4 -> integer
ip4_to_int() {
  local IFS=.
  set -- $1
  echo $(( ($1 << 24) | ($2 << 16) | ($3 << 8) | $4 ))
}

# Host IPv4 networks, one "<network-int> <mask-int>" per line. macOS ifconfig
# prints the mask as hex ("inet 192.168.0.10 netmask 0xffffff00"); point-to-point
# entries (VPN utuns) carry "--> peer" in field 4 and fail the 0x guard — intended:
# a guest VM cannot sit on a point-to-point link, so its subnet is never a
# candidate, whatever its mask. Host loopback and link-local entries are skipped
# so a guest's own 127./169.254. lines can never match them.
host_networks() {
  ifconfig -a 2>/dev/null \
    | awk '$1 == "inet" && $2 !~ /^127\./ && $2 !~ /^169\.254\./ { print $2, $4 }' \
    | while read -r ip mask; do
        case "$mask" in 0x*) ;; *) continue ;; esac
        mask=$(( mask ))
        echo "$(( $(ip4_to_int "$ip") & mask )) $mask"
      done
}

CANDIDATES="$(utmctl ip-address "$VM" 2>/dev/null | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' || true)"
if [ -z "$CANDIDATES" ]; then
  exit 0
fi

NETS="$(host_networks)"

while read -r cand; do
  cand_int="$(ip4_to_int "$cand")"
  while read -r net mask; do
    if [ -z "$net" ]; then
      continue
    fi
    if [ "$(( cand_int & mask ))" -eq "$net" ]; then
      echo "$cand"
      exit 0
    fi
  done <<<"$NETS"
done <<<"$CANDIDATES"

exit 0
