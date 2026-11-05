#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Restart every GitHub Actions runner service on the KVM runner host, first
# clearing what a dead job leaves behind (VMs, their disks) and repairing runner
# files that a full disk truncated.
#
# Safe to run when the host is healthy; it stops running jobs, so it is a recovery
# tool, not something to run casually.

set -uo pipefail

HOST_ROOT="${HOST_ROOT:-/home/github-runner}"
STOP_TIMEOUT="${STOP_TIMEOUT:-60}"   # seconds to wait for each runner to stop
START_RETRIES="${START_RETRIES:-3}"
MIN_FREE_GB="${MIN_FREE_GB:-15}"

# Only one restart at a time.
exec 9>/var/lock/runner-restart.lock
flock -n 9 || { echo "Another restart.sh is already running."; exit 1; }

mapfile -t runner_units < <(
  systemctl list-unit-files --type=service --no-legend 'actions.runner.*.service' |
    awk '{print $1}' | sort
)
if ((${#runner_units[@]} == 0)); then
  echo "No GitHub Actions runner services found."
  exit 1
fi

# The runner directory behind a service, taken from its ExecStart path.
unit_runner_dir() {
  local exec_path
  exec_path="$(systemctl show -p ExecStart --value "$1" 2>/dev/null | grep -o 'path=[^ ;]*' | head -1 | cut -d= -f2)"
  [[ -n "${exec_path}" ]] && dirname "${exec_path}"
}

# 1. Stop runners (in parallel) and wait until each is really inactive.
echo "Stopping ${#runner_units[@]} runner services"
sudo systemctl stop --no-block "${runner_units[@]}" || true
for unit in "${runner_units[@]}"; do
  for ((i = 0; i < STOP_TIMEOUT; i++)); do
    state=$(systemctl is-active "$unit" 2>/dev/null || true)
    [[ "$state" == "active" || "$state" == "deactivating" ]] || break
    sleep 1
  done
  state=$(systemctl is-active "$unit" 2>/dev/null || true)
  if [[ "$state" == "active" || "$state" == "deactivating" ]]; then
    echo "WARN: $unit still $state after ${STOP_TIMEOUT}s, killing it"
    sudo systemctl kill --signal=SIGKILL "$unit" || true
  fi
done

# 2. Kill leftover VMs and confirm they are gone. QEMU is started with nohup, so it
#    outlives its runner. The [q] stops pgrep matching this script itself.
vm_pattern='[q]emu-system-x86_64.*weaver-vm-'
if pgrep -f "$vm_pattern" >/dev/null; then
  echo "Stopping leftover QEMU VMs"
  sudo pkill -f "$vm_pattern" || true
  for _ in 1 2 3 4 5; do pgrep -f "$vm_pattern" >/dev/null || break; sleep 1; done
  sudo pkill -9 -f "$vm_pattern" || true
  sleep 2
  if pgrep -f "$vm_pattern" >/dev/null; then
    echo "ERROR: QEMU VMs still running, refusing to delete their disks:"
    pgrep -af "$vm_pattern" | cut -c1-160
    exit 1
  fi
fi

# 3. Remove leftover VM directories (nothing can be using them now).
sudo rm -rf "${HOST_ROOT}"/*/_work/_temp/weaver-vm-*

echo "Free disk: $(df -h --output=avail "${HOST_ROOT}" | tail -1 | tr -d ' ')"
if (($(df -Pk "${HOST_ROOT}" | awk 'NR==2 {print int($4/1024/1024)}') < MIN_FREE_GB)); then
  echo "ERROR: less than ${MIN_FREE_GB} GB free after cleanup; something else is using the disk."
  sudo du -xh --max-depth=2 "${HOST_ROOT}" 2>/dev/null | sort -h | tail -10
  exit 1
fi

# 4. Repair runner files a full disk left empty. runsvc.sh is a copy of bin/runsvc.sh;
#    an empty one makes systemd fail with "Exec format error" (status 203/EXEC).
#    .runner and .credentials hold the registration and cannot be regenerated here.
failed=0
for unit in "${runner_units[@]}"; do
  dir="$(unit_runner_dir "$unit")"
  if [[ -z "${dir}" || ! -d "${dir}" ]]; then
    echo "WARN: cannot find the runner directory for $unit, skipping repair"
    continue
  fi
  if [[ ! -s "${dir}/runsvc.sh" || ! -x "${dir}/runsvc.sh" ]]; then
    if [[ -s "${dir}/bin/runsvc.sh" ]]; then
      echo "Repairing ${dir}/runsvc.sh (was empty or not executable)"
      sudo cp "${dir}/bin/runsvc.sh" "${dir}/runsvc.sh" &&
        sudo chown --reference="${dir}/bin/runsvc.sh" "${dir}/runsvc.sh" &&
        sudo chmod 755 "${dir}/runsvc.sh" || { echo "ERROR: could not repair ${dir}/runsvc.sh"; failed=1; }
    else
      echo "ERROR: ${dir}/runsvc.sh and bin/runsvc.sh are both unusable; reconfigure this runner"
      failed=1
    fi
  fi
  for f in .runner .credentials .credentials_rsaparams; do
    if [[ -e "${dir}/${f}" && ! -s "${dir}/${f}" ]]; then
      echo "ERROR: ${dir}/${f} is empty; this runner must be reconfigured (config.sh remove, then config.sh)"
      failed=1
    fi
  done
done

# 5. Start every runner, retrying, and show why any that still fails.
for unit in "${runner_units[@]}"; do
  ok=0
  for ((attempt = 1; attempt <= START_RETRIES; attempt++)); do
    sudo systemctl reset-failed "$unit" 2>/dev/null || true
    if sudo systemctl start "$unit" && sleep 3 &&
      [[ "$(systemctl is-active "$unit")" == "active" ]]; then
      ok=1
      break
    fi
    echo "WARN: $unit not active (attempt $attempt/$START_RETRIES)"
    sleep $((attempt * 5))
  done
  if ((ok == 0)); then
    failed=1
    echo "ERROR: $unit failed to start. Last log lines:"
    sudo journalctl -u "$unit" -n 15 --no-pager
  fi
done

echo
echo "Runner service status:"
for unit in "${runner_units[@]}"; do
  state=$(systemctl is-active "$unit" 2>/dev/null || true)
  printf "%-75s %s\n" "$unit" "${state:-unknown}"
  [[ "$state" == "active" ]] || failed=1
done
exit "$failed"
