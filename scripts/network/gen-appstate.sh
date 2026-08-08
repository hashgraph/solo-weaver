#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Generate the three BN `application-state` JSON files from captured peer IPs.
#
# The block node reads these files to compose its statusz/inbound + statusz/outbound
# responses; authoring them is how the multi-VM network harness (taskfiles/network.yaml)
# drives statusz deterministically. See docs/dev/traffic-shaper-test-plan.md (local).
#
# JSON casing: these files use snake_case ("active_endpoints") and carry scheme/protocol
# fields — this is the BN's *input* format (per the real BN sample). It is deliberately
# NOT the same surface as the statusz HTTP *response* the daemon decodes, which is
# camelCase ("activeEndpoints", "tlsRequired") in internal/blocknode/shaper/statusz_client.go.
# The BN reads snake_case files and emits camelCase statusz; do not "fix" this to camelCase.
#
# Usage:
#   gen-appstate.sh <out-dir> <bn-ip> <publisher-ip> <partner-ip> \
#                   <public-ip> <backfill-ip> <restricted-ip>
#
# Ports come from the caller, which reads them out of the chart values the harness
# deploys with (test/config/network_uat_values.yaml) so the fixture cannot claim a
# listener the block node does not have. A wrong port here is invisible: the daemon
# programs it into the `<policy>_ports` set, real traffic on the true port then
# matches no rule and lands in the default class, and the run reads as a
# classification regression rather than a seeding bug.
#
# On the single-service topology every facility shares one port, so the fixture's
# four ports collapse to the same value and only the source-IP sets separate
# publisher/partner from public. Override individually for a split-topology run.
set -eu

if [ "$#" -ne 7 ]; then
  echo "usage: $0 <out-dir> <bn-ip> <publisher-ip> <partner-ip> <public-ip> <backfill-ip> <restricted-ip>" >&2
  exit 2
fi

OUT_DIR=$1
BN=$2
PUBLISHER=$3
PARTNER=$4
PUBLIC=$5
BACKFILL=$6
RESTRICTED=$7

BN_PORT=${BN_PORT:?BN_PORT is required (read from the harness chart values)}
PORT_PUBLISHER=${PORT_PUBLISHER:-$BN_PORT}
PORT_SUBSCRIBER=${PORT_SUBSCRIBER:-$BN_PORT}
PORT_BLOCKACCESS=${PORT_BLOCKACCESS:-$BN_PORT}
PORT_STATUS=${PORT_STATUS:-$BN_PORT}
BACKFILL_PEER_PORT=${BACKFILL_PEER_PORT:-50980}  # the peer-BN API port for the outbound backfill entry

mkdir -p "$OUT_DIR"

# Emit one NetworkConnection object (matches network-data.proto shape).
# Args: <laddr> <lport> <raddr> <rport> <category>
conn() {
  printf '    {\n'
  printf '      "local": { "address": "%s", "port": "%s" },\n' "$1" "$2"
  printf '      "remote": { "address": "%s", "port": "%s" },\n' "$3" "$4"
  printf '      "category": "%s",\n' "$5"
  printf '      "scheme": "grpc",\n'
  printf '      "protocol": "TCP"\n'
  printf '    }'
}

# known-publishers.json -> feeds statusz/inbound (publisher roster).
{
  printf '{\n  "active_endpoints": [\n'
  conn "$BN" "$PORT_PUBLISHER" "$PUBLISHER" "*" "publisher"
  printf '\n  ]\n}\n'
} >"$OUT_DIR/known-publishers.json"

# inbound-partners.json -> feeds statusz/inbound (full inbound roster).
# public entries carry an empty remote.address (fallthrough, ports-only).
{
  printf '{\n  "active_endpoints": [\n'
  conn "$BN" "$PORT_PUBLISHER" "$PUBLISHER" "*" "publisher"; printf ',\n'
  conn "$BN" "$PORT_SUBSCRIBER" "$PARTNER" "*" "partner"; printf ',\n'
  conn "$BN" "$PORT_SUBSCRIBER" "" "" "public"; printf ',\n'
  conn "$BN" "$PORT_BLOCKACCESS" "" "" "public"; printf ',\n'
  conn "$BN" "$PORT_STATUS" "" "" "public"; printf ',\n'
  conn "$BN" "$PORT_SUBSCRIBER" "$RESTRICTED" "*" "restricted"
  printf '\n  ]\n}\n'
} >"$OUT_DIR/inbound-partners.json"

# outbound-partners.json -> feeds statusz/outbound (peer-BN backfill, ip:port).
{
  printf '{\n  "active_endpoints": [\n'
  conn "$BN" "*" "$BACKFILL" "$BACKFILL_PEER_PORT" "partner"
  printf '\n  ]\n}\n'
} >"$OUT_DIR/outbound-partners.json"

echo "wrote known-publishers.json, inbound-partners.json, outbound-partners.json to $OUT_DIR"
