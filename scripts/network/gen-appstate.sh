#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Generate the three BN `application-state` JSON files from captured peer IPs.
#
# The block node reads these files to compose its statusz/inbound + statusz/outbound
# responses; authoring them is how the multi-VM network harness (taskfiles/network.yaml)
# drives statusz deterministically. See docs/dev/traffic-shaper-test-plan.md (local).
#
# Usage:
#   gen-appstate.sh <out-dir> <bn-ip> <publisher-ip> <partner-ip> \
#                   <public-ip> <backfill-ip> <restricted-ip>
#
# Ports are the current per-deployment BN listener assignments:
#   publisher 40984 | subscriber 40980 | block-access 40981 | server-status 40982
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

PORT_PUBLISHER=40984
PORT_SUBSCRIBER=40980
PORT_BLOCKACCESS=40981
PORT_STATUS=40982
BACKFILL_PEER_PORT=50980  # the peer-BN API port for the outbound backfill entry

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
