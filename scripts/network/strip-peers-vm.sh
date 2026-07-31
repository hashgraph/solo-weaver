#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Right-size and de-conflict the cloned "peers" UTM VM (taskfiles/network.yaml).
#
# A freshly `utmctl clone`d VM inherits the golden's exact config, including its MAC
# address. On a bridged UTM network that means the peers VM and the BN VM present the
# SAME MAC and fight over one DHCP lease — the peers VM either never gets an IP or
# steals the BN's. This script gives the peers VM a distinct, locally-administered MAC
# and trims its vCPU/RAM to the minimum needed to source traffic.
#
# UTM caches each VM's config in memory while the app is running and rewrites
# config.plist on quit/state changes, clobbering external edits. So we QUIT UTM first
# to release the files, edit with `plutil`, then reopen UTM so it reloads from disk.
#
# The MAC edit is idempotent: if the VM already has a locally-administered MAC (02:...)
# we keep it, so re-running network:up on an existing peers VM does not churn its IP.
#
# Usage:
#   strip-peers-vm.sh <config.plist-path> <cpu-count> <mem-mib>
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <config.plist-path> <cpu-count> <mem-mib>" >&2
  exit 2
fi

PLIST=$1
CPUS=$2
MEM_MIB=$3

if [ ! -f "$PLIST" ]; then
  echo "❌ config.plist not found: $PLIST" >&2
  exit 1
fi

# Locally-administered (02:...), unicast MAC generator.
new_mac() {
  hexdump -vn5 -e '5/1 ":%02x"' /dev/urandom | sed 's/^:/02:/'
}

# Preflight: confirm this UTM version's config.plist exposes the keys we intend to edit
# BEFORE we disturb any running VMs or quit UTM. Stopping every VM + quitting UTM is
# disruptive (it also stops unrelated VMs, e.g. the BN working VM), so if the schema
# differs — the right-sizing keys are not readable — no-op with a warning rather than
# paying that cost for an edit we cannot apply. The caller (network:up) then starts the
# peers VM at the golden's full CPU/RAM: larger than ideal, but functional.
pf_cpu="$(plutil -extract System.CPUCount raw -o - "$PLIST" 2>/dev/null || echo "")"
pf_mem="$(plutil -extract System.MemorySize raw -o - "$PLIST" 2>/dev/null || echo "")"
if [ -z "$pf_cpu" ] || [ -z "$pf_mem" ]; then
  echo "⚠️  config.plist does not expose System.CPUCount / System.MemorySize on this UTM version" >&2
  echo "    (CPUCount='${pf_cpu:-<missing>}', MemorySize='${pf_mem:-<missing>}'). Skipping the" >&2
  echo "    resource strip: no VMs stopped, UTM left running. Right-size the peers VM manually" >&2
  echo "    in the UTM app, or update this script for your UTM config schema." >&2
  exit 0
fi

# Gracefully stop every running VM BEFORE quitting UTM, then quit so it releases the
# config files. Quitting the UTM app hard-stops (plug-pulls) any still-running VM,
# which can corrupt guest files mid-write (e.g. NUL-padded /etc/passwd, a truncated
# just-installed binary) — including unrelated VMs such as the BN working VM.
# IMPORTANT: `utmctl stop` defaults to --force (a hard power-off = same plug-pull), so
# we must pass --request to ask the guest OS for a clean ACPI shutdown, then wait for
# each VM to actually power down before quitting UTM.
echo "Gracefully stopping any running VMs (ACPI) before quitting UTM..."
running_vms() { utmctl list 2>/dev/null | awk 'NR>1 && tolower($2)=="started" {print $1}'; }
for uuid in $(running_vms); do
  echo "  requesting shutdown of $uuid"
  utmctl stop --request "$uuid" >/dev/null 2>&1 || true
done
# Wait (bounded, ~2min) for the guest shutdowns to complete so nothing is running at quit.
for _ in $(seq 1 60); do
  [ -z "$(running_vms)" ] && break
  sleep 2
done
if [ -n "$(running_vms)" ]; then
  echo "  ⚠️  some VMs did not shut down gracefully in time; NOT force-quitting to avoid"
  echo "      corrupting them. Stop them manually, then re-run. Aborting the strip."
  exit 1
fi

echo "Quitting UTM to release VM config files..."
osascript -e 'tell application "UTM" to quit' >/dev/null 2>&1 || true
sleep 3

# MAC: only regenerate if the VM still carries a non-locally-administered address
# (i.e. it is still the golden's cloned MAC and hasn't been de-conflicted yet).
cur_mac="$(plutil -extract Network.0.MacAddress raw -o - "$PLIST" 2>/dev/null || echo "")"
case "$cur_mac" in
  [0][2]:*)
    echo "  MAC already locally-administered ($cur_mac) — keeping it"
    ;;
  *)
    mac="$(new_mac)"
    plutil -replace Network.0.MacAddress -string "$mac" "$PLIST" \
      && echo "  set MAC=$mac (was ${cur_mac:-<none>})" \
      || echo "  ⚠️  could not set MacAddress — check UTM config schema"
    ;;
esac

plutil -replace System.CPUCount -integer "$CPUS" "$PLIST" \
  && echo "  set CPUCount=$CPUS" \
  || echo "  ⚠️  could not set CPUCount — check UTM config schema"

plutil -replace System.MemorySize -integer "$MEM_MIB" "$PLIST" \
  && echo "  set MemorySize=${MEM_MIB}MiB" \
  || echo "  ⚠️  could not set MemorySize — check UTM config schema"

# Report the applied values so the caller can confirm.
echo "peers VM config now: MAC=$(plutil -extract Network.0.MacAddress raw -o - "$PLIST" 2>/dev/null || echo '?')" \
     "CPUs=$(plutil -extract System.CPUCount raw -o - "$PLIST" 2>/dev/null || echo '?')" \
     "RAM=$(plutil -extract System.MemorySize raw -o - "$PLIST" 2>/dev/null || echo '?')MiB"

# Reopen UTM so it picks up the new config and utmctl works again.
echo "Reopening UTM..."
open -a UTM
sleep 5
