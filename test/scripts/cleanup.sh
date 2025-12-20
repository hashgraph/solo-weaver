#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Purpose: Unmount and remove /opt/solo... paths found in input if any. This is needed if reset wasn't able to fully clean up.
# This is dangerous if used improperly; only use on trusted input.
# Usage: cleanup.sh [-n] [-f file]
#   -n    dry-run (show actions only)
#   -f    read lines from file (otherwise reads stdin)
# e.g cat logfile.txt | ./cleanup.sh -n
set -euo pipefail

DRY_RUN=0
INPUT_FILE=""

usage() {
  cat <<EOF
Usage: $0 [-n] [-f file]
 -n    dry-run (show actions only)
 -f    read lines from file (otherwise reads stdin)
EOF
  exit 1
}

while getopts "nf:" opt; do
  case "$opt" in
    n) DRY_RUN=1 ;;
    f) INPUT_FILE="${OPTARG}" ;;
    *) usage ;;
  esac
done
shift $((OPTIND-1))

if [ -n "$INPUT_FILE" ]; then
  INPUT="$(cat "$INPUT_FILE")"
else
  INPUT="$(cat -)"
fi

# extract paths quoted like '.../opt/solo...' and any unquoted /opt/solo... tokens
paths_raw=()
while IFS= read -r line; do
  # quoted single-quote path
  if p=$(printf "%s\n" "$line" | sed -n "s/.*'\(\/opt\/solo[^']*\)'.*/\1/p"); then
    [ -n "$p" ] && paths_raw+=("$p")
  fi
  # unquoted occurrences
  while read -r m; do paths_raw+=("$m"); done < <(printf "%s\n" "$line" | grep -oE '/opt/solo[^[:space:]'\''"]*' 2>/dev/null || true)
done <<< "$INPUT"

# dedupe preserving order
declare -A seen
paths=()
for p in "${paths_raw[@]}"; do
  if [ -z "${seen[$p]:-}" ]; then
    seen[$p]=1
    paths+=("$p")
  fi
done

if [ ${#paths[@]} -eq 0 ]; then
  echo "No /opt/solo paths found in input."
  exit 0
fi

# helper: detect mountpoint
get_mountpoint() {
  local path=$1
  if command -v findmnt >/dev/null 2>&1; then
    findmnt -n -T "$path" -o TARGET 2>/dev/null || true
  else
    # df fallback (may vary across systems)
    df -P "$path" 2>/dev/null | awk 'NR>1{print $6; exit}' || true
  fi
}

for p in "${paths[@]}"; do
  case "$p" in
   /opt/solo|/opt/solo/* ) ;;
    * ) echo "Skipping unsafe path: $p"; continue ;;
  esac

  echo "----"
  echo "Path: $p"

  echo "Processes holding files under \`$p\`:"
  if command -v lsof >/dev/null 2>&1; then
    sudo lsof +D "$p" 2>/dev/null || true
  else
    sudo fuser -mv "$p" 2>/dev/null || true
  fi

  mp=$(get_mountpoint "$p" || true)
  if [ -n "$mp" ]; then
    echo "Detected mountpoint: \`$mp\`"
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "[dry-run] sudo umount -l \"$mp\""
    else
      echo "Running: sudo umount -l \"$mp\" || true"
      sudo umount -l "$mp" 2>/dev/null || echo "umount failed for $mp (may not be a mountpoint)"
    fi
  else
    echo "No mountpoint detected; will attempt lazy unmount on the path itself."
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "[dry-run] sudo umount -l \"$p\" || true"
    else
      sudo umount -l "$p" 2>/dev/null || true
    fi
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] sudo rm -rf \"$p\""
  else
    echo "Removing: sudo rm -rf \"$p\""
    sudo rm -rf "$p" || echo "rm failed for $p (still busy?)"
  fi
done

echo "Done."

