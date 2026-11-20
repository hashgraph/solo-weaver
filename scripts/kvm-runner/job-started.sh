#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
#
# Runner job-started hook (ACTIONS_RUNNER_HOOK_JOB_STARTED) for the KVM runner host.
#
# Runs before the job's first step, so before checkout. It stops QEMU VMs that an
# earlier job left behind and removes their working directories. The sweep inside
# the kvm-vm-up action cannot do this when the disk is full, because checkout fails
# first and the action never runs.
#
# A VM is an orphan when no Runner.Worker is alive for the runner that owns it:
# a live job always has a live worker, so a VM with none belongs to a dead job.
# The runner that is starting this job is its own case: it has a worker (this
# one), but the job has not booted a VM yet, so any VM under its directory is left
# over from an earlier job. A VM older than STALE_AGE_MINUTES is swept regardless,
# to cover a runner whose worker is alive but wedged.
#
# This script always exits 0. A cleanup failure must not fail the job.
#
# Install: see scripts/kvm-runner/README.md.

set -uo pipefail

HOST_ROOT="${HOST_ROOT:-/home/github-runner}"
STALE_AGE_MINUTES="${STALE_AGE_MINUTES:-240}"
ORPHAN_GRACE_MINUTES="${ORPHAN_GRACE_MINUTES:-2}"

log() { echo "[weaver-job-started] $*" >&2; }

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

# The runner directory whose Runner.Worker is running this hook, if we can tell.
own_runner_dir() {
  local pid="$$" args dir
  while [ -n "${pid}" ] && [ "${pid}" -gt 1 ]; do
    args="$(ps -o args= -p "${pid}" 2>/dev/null || true)"
    case "${args}" in
      "${HOST_ROOT}"/*/bin/Runner.Worker*)
        dir="${args%%/bin/Runner.Worker*}"
        echo "${dir}"
        return 0
        ;;
    esac
    pid="$(ps -o ppid= -p "${pid}" 2>/dev/null | tr -d ' ')"
  done
  return 1
}

# Runner directories that have a live Runner.Worker.
live_runner_dirs() {
  ps -eo args= | while read -r cmd rest; do
    case "${cmd}" in
      "${HOST_ROOT}"/*/bin/Runner.Worker) echo "${cmd%/bin/Runner.Worker}" ;;
    esac
  done | sort -u
}

main() {
  local own live pid etime cmd rest runner_dir age file vm_dir
  own="$(own_runner_dir || true)"
  live="$(live_runner_dirs)"
  log "runner dir: ${own:-unknown}; runners with a live worker: $(echo "${live}" | tr '\n' ' ')"

  local stop_pids=""
  while read -r pid etime cmd rest; do
    case "$(basename -- "${cmd}")" in
      qemu-system-*) ;;
      *) continue ;;
    esac
    file="$(printf '%s' "${rest}" | grep -o "file=${HOST_ROOT}/[^,[:space:]]*/_work/_temp/weaver-vm-[^/,[:space:]]*" | head -1 || true)"
    [ -n "${file}" ] || continue
    vm_dir="${file#file=}"
    runner_dir="${vm_dir%/_work/_temp/weaver-vm-*}"
    age="$(etime_minutes "${etime}")"

    if [ "${runner_dir}" = "${own}" ]; then
      log "orphan (earlier job on this runner): pid ${pid}, ${vm_dir}"
      stop_pids="${stop_pids} ${pid}"
    elif ! printf '%s\n' "${live}" | grep -qxF "${runner_dir}" && [ "${age}" -ge "${ORPHAN_GRACE_MINUTES}" ]; then
      log "orphan (no live worker for ${runner_dir}): pid ${pid}, ${vm_dir}"
      stop_pids="${stop_pids} ${pid}"
    elif [ "${age}" -ge "${STALE_AGE_MINUTES}" ]; then
      log "stale (${age} min old, over ${STALE_AGE_MINUTES}): pid ${pid}, ${vm_dir}"
      stop_pids="${stop_pids} ${pid}"
    fi
  done < <(ps -eo pid=,etime=,args=)

  if [ -n "${stop_pids}" ]; then
    # shellcheck disable=SC2086
    kill ${stop_pids} 2>/dev/null || true
    local alive="${stop_pids}" still
    for _ in 1 2 3 4 5; do
      still=""
      for pid in ${alive}; do
        if kill -0 "${pid}" 2>/dev/null; then still="${still} ${pid}"; fi
      done
      alive="${still}"
      [ -n "${alive}" ] || break
      sleep 2
    done
    # shellcheck disable=SC2086
    [ -z "${alive}" ] || kill -9 ${alive} 2>/dev/null || true
  fi

  # Remove VM directories nothing is using any more: no QEMU has the disk open, and
  # the directory belongs to this runner, a runner with no live worker, or is stale.
  local dir owner running
  running="$(ps -eo args=)"
  for dir in "${HOST_ROOT}"/*/_work/_temp/weaver-vm-*; do
    [ -d "${dir}" ] && [ ! -L "${dir}" ] || continue
    case "${running}" in *"file=${dir}/"*) continue ;; esac
    owner="${dir%/_work/_temp/weaver-vm-*}"
    if [ "${owner}" = "${own}" ] \
      || ! printf '%s\n' "${live}" | grep -qxF "${owner}" \
      || [ -n "$(find "${dir}" -maxdepth 0 -mmin "+${STALE_AGE_MINUTES}" 2>/dev/null)" ]; then
      log "removing VM directory ${dir} ($(du -sh "${dir}" 2>/dev/null | cut -f1))"
      rm -rf -- "${dir}"
    fi
  done

  log "free disk: $(df -h --output=avail "${HOST_ROOT}" 2>/dev/null | tail -1 | tr -d ' ')"
}

main || log "cleanup hit an error; continuing"
exit 0
