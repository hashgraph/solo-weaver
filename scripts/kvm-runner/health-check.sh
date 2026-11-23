#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Health check for the KVM runner host. Prints one line per finding
# ("OK|WARN|CRIT <check>: <detail>") and exits 0 (all OK), 1 (warnings) or 2 (critical).
#
#   health-check.sh                  print findings
#   health-check.sh --annotations    also print GitHub ::warning::/::error:: annotations
#   health-check.sh --summary        also append a table to $GITHUB_STEP_SUMMARY (if set)
#   health-check.sh --notify         POST to $WEBHOOK_URL when the status changes (for cron)
#
# Thresholds (environment): DISK_WARN_PCT=80 DISK_CRIT_PCT=90 MEM_AVAIL_WARN_GB=3
# LOAD_WARN_FACTOR=1.5 (5-min load per vCPU) VM_STALE_MINUTES=240 MAX_VMS=4
# ORPHAN_GRACE_MINUTES=2 HOST_ROOT=/home/github-runner
#
# From the workflows it never fails the job; the caller discards the exit code.

set -uo pipefail

HOST_ROOT="${HOST_ROOT:-/home/github-runner}"
DISK_WARN_PCT="${DISK_WARN_PCT:-80}"
DISK_CRIT_PCT="${DISK_CRIT_PCT:-90}"
MEM_AVAIL_WARN_GB="${MEM_AVAIL_WARN_GB:-3}"
LOAD_WARN_FACTOR="${LOAD_WARN_FACTOR:-1.5}"
VM_STALE_MINUTES="${VM_STALE_MINUTES:-240}"
MAX_VMS="${MAX_VMS:-4}"
ORPHAN_GRACE_MINUTES="${ORPHAN_GRACE_MINUTES:-2}"
STATE_FILE="${STATE_FILE:-/var/tmp/weaver-health.state}"

ANNOTATIONS=0 SUMMARY=0 NOTIFY=0
for arg in "$@"; do
  case "${arg}" in
    --annotations) ANNOTATIONS=1 ;;
    --summary) SUMMARY=1 ;;
    --notify) NOTIFY=1 ;;
    *) echo "unknown argument: ${arg}" >&2; exit 64 ;;
  esac
done

LINES=()
STATUS=0
add() { # level check detail
  local level="$1" check="$2" detail="$3"
  LINES+=("${level}|${check}|${detail}")
  case "${level}" in
    WARN) [ "${STATUS}" -ge 1 ] || STATUS=1 ;;
    CRIT) STATUS=2 ;;
  esac
}

etime_minutes() {
  local t="$1" days=0 IFS=:
  case "${t}" in
    *-*) days="${t%%-*}"; t="${t#*-}" ;;
  esac
  # shellcheck disable=SC2086
  set -- ${t}
  case $# in
    3) echo $((10#${days} * 1440 + 10#$1 * 60 + 10#$2)) ;;
    2) echo $((10#${days} * 1440 + 10#$1)) ;;
    *) echo 0 ;;
  esac
}

# --- disk ---
read -r pct avail_kb < <(df -Pk "${HOST_ROOT}" 2>/dev/null | awk 'NR==2 {gsub("%","",$5); print $5, $4}')
if [ -n "${pct:-}" ]; then
  avail_gb=$((avail_kb / 1024 / 1024))
  detail="${pct}% used, ${avail_gb} GB free"
  if [ "${pct}" -ge "${DISK_CRIT_PCT}" ]; then add CRIT disk "${detail}"
  elif [ "${pct}" -ge "${DISK_WARN_PCT}" ]; then add WARN disk "${detail}"
  else add OK disk "${detail}"; fi
else
  add WARN disk "could not read disk usage for ${HOST_ROOT}"
fi

# --- memory ---
if [ -r /proc/meminfo ]; then
  avail_gb=$(awk '/MemAvailable/ {print int($2 / 1024 / 1024)}' /proc/meminfo)
  detail="${avail_gb} GB available"
  if [ "${avail_gb}" -lt "${MEM_AVAIL_WARN_GB}" ]; then add WARN memory "${detail}"; else add OK memory "${detail}"; fi
fi

# --- load ---
if [ -r /proc/loadavg ]; then
  load5=$(awk '{print $2}' /proc/loadavg)
  cpus=$(nproc 2>/dev/null || echo 1)
  detail="5-min load ${load5} on ${cpus} vCPUs"
  if awk -v l="${load5}" -v c="${cpus}" -v f="${LOAD_WARN_FACTOR}" 'BEGIN {exit !(l > c * f)}'; then add WARN load "${detail}"; else add OK load "${detail}"; fi
fi

# --- runner services ---
if command -v systemctl >/dev/null 2>&1; then
  total=0 bad=""
  while read -r unit state; do
    [ -n "${unit}" ] || continue
    total=$((total + 1))
    [ "${state}" = "active" ] || bad="${bad} ${unit#actions.runner.}(${state})"
  done < <(systemctl list-units --all --type=service --no-legend 'actions.runner.*' 2>/dev/null | awk '{print $1, $3}')
  if [ "${total}" -eq 0 ]; then add WARN runners "no runner services found"
  elif [ -n "${bad}" ]; then add CRIT runners "not active:${bad}"
  else add OK runners "${total} runner services active"; fi
fi

# --- runner files a full disk can leave empty ---
empty=""
for dir in "${HOST_ROOT}"/actions-runner*/; do
  [ -d "${dir}" ] || continue
  for f in runsvc.sh .runner .credentials .credentials_rsaparams; do
    if [ -e "${dir}${f}" ] && [ ! -s "${dir}${f}" ]; then empty="${empty} ${dir#"${HOST_ROOT}"/}${f}"; fi
  done
done
if [ -n "${empty}" ]; then add CRIT runner-files "empty:${empty}"; else add OK runner-files "no empty runner files"; fi

# --- VMs ---
live_workers="$(ps -eo args= | awk -v root="${HOST_ROOT}" 'index($1, root "/") == 1 && $1 ~ /\/bin\/Runner\.Worker$/ {sub("/bin/Runner.Worker$", "", $1); print $1}' | sort -u)"
vms=0 orphans="" stale=""
while read -r pid etime cmd rest; do
  case "$(basename -- "${cmd}")" in qemu-system-*) ;; *) continue ;; esac
  file="$(printf '%s' "${rest}" | grep -o "file=${HOST_ROOT}/[^,[:space:]]*/_work/_temp/weaver-vm-[^/,[:space:]]*" | head -1 || true)"
  [ -n "${file}" ] || continue
  vms=$((vms + 1))
  runner_dir="${file#file=}"
  runner_dir="${runner_dir%/_work/_temp/weaver-vm-*}"
  age="$(etime_minutes "${etime}")"
  if [ "${age}" -ge "${VM_STALE_MINUTES}" ]; then
    stale="${stale} pid${pid}(${age}m)"
  elif [ "${age}" -ge "${ORPHAN_GRACE_MINUTES}" ] && ! printf '%s\n' "${live_workers}" | grep -qxF "${runner_dir}"; then
    orphans="${orphans} pid${pid}(${runner_dir##*/})"
  fi
done < <(ps -eo pid=,etime=,args=)

if [ -n "${stale}" ]; then add CRIT vms "older than ${VM_STALE_MINUTES} min:${stale}"
elif [ -n "${orphans}" ]; then add WARN vms "no live runner worker:${orphans}"
elif [ "${vms}" -gt "${MAX_VMS}" ]; then add WARN vms "${vms} running, above the cap of ${MAX_VMS}"
else add OK vms "${vms} running (cap ${MAX_VMS})"; fi

# --- output ---
for entry in "${LINES[@]}"; do
  IFS='|' read -r level check detail <<<"${entry}"
  printf '%-4s %s: %s\n' "${level}" "${check}" "${detail}"
  if [ "${ANNOTATIONS}" -eq 1 ]; then
    case "${level}" in
      WARN) echo "::warning title=KVM host health (${check})::${detail}" ;;
      CRIT) echo "::error title=KVM host health (${check})::${detail}" ;;
    esac
  fi
done

if [ "${SUMMARY}" -eq 1 ] && [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "### KVM host health"
    echo
    echo "| Status | Check | Detail |"
    echo "|---|---|---|"
    for entry in "${LINES[@]}"; do
      IFS='|' read -r level check detail <<<"${entry}"
      echo "| ${level} | ${check} | ${detail} |"
    done
  } >>"${GITHUB_STEP_SUMMARY}"
fi

if [ "${NOTIFY}" -eq 1 ]; then
  prev="$(cat "${STATE_FILE}" 2>/dev/null || echo 0)"
  if [ "${STATUS}" != "${prev}" ] && [ -n "${WEBHOOK_URL:-}" ]; then
    text="KVM runner $(hostname): status $([ "${STATUS}" -eq 0 ] && echo recovered || echo "needs attention")"
    for entry in "${LINES[@]}"; do
      case "${entry}" in OK\|*) ;; *) text="${text}"$'\n'"${entry//|/ }" ;; esac
    done
    curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
      -d "$(printf '{"text": %s}' "$(printf '%s' "${text}" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null || printf '"%s"' "KVM runner status changed")")" \
      "${WEBHOOK_URL}" >/dev/null || echo "WARN: could not post to the webhook" >&2
  fi
  echo "${STATUS}" >"${STATE_FILE}" 2>/dev/null || true
fi

exit "${STATUS}"
