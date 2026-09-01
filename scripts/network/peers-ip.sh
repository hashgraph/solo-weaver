#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Print the bridged LAN IPv4 of a UTM VM, or nothing until one is usable.
#
# `utmctl ip-address` lists every guest interface in arbitrary order — docker0,
# compose bridges, cilium_host — so taking the first is a coin flip. Select by the
# property callers need instead: the guest address inside one of the HOST's own
# interface subnets. An address in a range the guest uses internally (docker
# pools, pod CIDR) can only match when the host's LAN overlaps it, so those are
# returned only after answering SSH. Empty means nothing usable yet, so wait
# loops keep polling.
#
# Cannot disambiguate the peers VM: its five macvlan role addresses share the LAN
# /24 and all answer sshd, so the first listed wins — hosts.env from network:up is
# the authority there. Nor a host LAN wholly inside 172.16/12, where the guest's
# real and internal addresses are both guest-internal and ordering cannot separate
# them.
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

# Host IPv4 networks, one "<network-int> <mask-int>" per line. The mask is hex on
# macOS ("inet 192.168.0.10 netmask 0xffffff00"); point-to-point entries (VPN
# utuns) put "--> peer" in field 4 and fail the 0x guard, which is intended — a
# guest cannot sit on a point-to-point link. Skipping the host's own loopback and
# link-local entries is what keeps a guest's 127./169.254. from ever matching.
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

# `|| true`: without it a failing ifconfig kills this script and, silently, the
# caller assigning from it under `set -e`.
NETS="$(host_networks || true)"
if [ -z "$NETS" ]; then
  echo "peers-ip.sh: no usable IPv4 on any host interface — nothing to match a guest address against" >&2
  exit 0
fi

# Docker's pools and the cluster pod CIDR. This gates whether a match is worth
# verifying and orders the probe, never what to return, so a stale list costs a
# skipped check rather than a wrong address.
GUEST_INTERNAL='^(172\.(1[6-9]|2[0-9]|3[01])\.|10\.[4-7]\.)'

# Candidates inside one of the host's networks, one per line. The break stops a
# candidate matching two host networks from being emitted twice.
subnet_matches() {
  while read -r cand; do
    cand_int="$(ip4_to_int "$cand")"
    while read -r net mask; do
      if [ -z "$net" ]; then
        continue
      fi
      if [ "$(( cand_int & mask ))" -eq "$net" ]; then
        echo "$cand"
        break
      fi
    done <<<"$NETS"
  done <<<"$CANDIDATES"
}

# Deduplicated, and guest-internal addresses moved last: on a host whose LAN
# overlaps a docker pool, the guest's docker0 address is also a real address on
# that LAN — often the gateway, which answers :22 — so it must never outrank the
# guest's own address in the probe order.
MATCHES="$(subnet_matches | awk -v gi="$GUEST_INTERNAL" '
  !seen[$0]++ { if ($0 ~ gi) tail = tail $0 "\n"; else head = head $0 "\n" }
  END { printf "%s%s", head, tail }')"
if [ -z "$MATCHES" ]; then
  exit 0
fi

# `-G` alone: bare `nc -z` hangs ~75s on a blackholed address, and `-z` with
# either `-G` or `-w` reports failure even for an open port. `-G` is BSD-only, so
# a GNU nc (or none at all) leaves nothing verifiable — accept the candidate
# rather than strand the caller.
probe_ssh() {
  case "$(nc -h 2>&1 || true)" in *-G*) ;; *) return 0 ;; esac
  nc -G 2 "$1" 22 </dev/null >/dev/null 2>&1
}

# A lone match answers without touching the network, unless it is guest-internal —
# that is the docker0 answer this script exists to avoid, so verify it first.
if [ "$(echo "$MATCHES" | wc -l)" -eq 1 ]; then
  if echo "$MATCHES" | grep -Eq "$GUEST_INTERNAL" && ! probe_ssh "$MATCHES"; then
    exit 0
  fi
  echo "$MATCHES"
  exit 0
fi

while read -r cand; do
  if probe_ssh "$cand"; then
    echo "$cand"
    exit 0
  fi
done <<<"$MATCHES"

echo "peers-ip.sh: $(echo "$MATCHES" | tr '\n' ' ')matched a host subnet but none answered SSH — guest still booting?" >&2
exit 0
