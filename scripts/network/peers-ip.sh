#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Print the bridged LAN IPv4 of a UTM VM, or nothing until DHCP assigns it.
#
# The golden image runs Docker, so the guest also carries a docker0 address
# (172.17.0.1); `utmctl ip-address` lists every interface. We skip docker
# (172.17-172.31), loopback (127.) and link-local (169.254.) so callers get the
# bridged address they actually SSH to — and get an EMPTY result (not docker0)
# while the real NIC is still waiting for its lease, so wait loops keep polling.
#
# Usage: peers-ip.sh <vm-name>
set -euo pipefail

VM="${1:?usage: peers-ip.sh <vm-name>}"

utmctl ip-address "$VM" 2>/dev/null \
  | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' \
  | grep -Ev '^(127\.|169\.254\.|172\.(1[7-9]|2[0-9]|3[01])\.)' \
  | head -1 || true
