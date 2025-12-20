#!/usr/bin/env bash
set -euo pipefail

# Fun banner and helpers
_bold=$(tput bold 2>/dev/null || echo "")
_reset=$(tput sgr0 2>/dev/null || echo "")
_red=$(tput setaf 1 2>/dev/null || echo "")
_green=$(tput setaf 2 2>/dev/null || echo "")
_yellow=$(tput setaf 3 2>/dev/null || echo "")

banner() {
  printf "%s\n" "${_yellow}🔥🔥🔥  RESET RITUAL  🔥🔥🔥${_reset}"
  printf "%s\n" "${_bold}${_red}Wiping the cluster cobwebs...${_reset}"
  echo
}

fun() {
  # usage: fun "message"
  printf "%s %s\n" "🔥" "$1"
}

banner

# safe command checks
if command -v helm >/dev/null 2>&1; then
  fun "Uninstalling MetallB via helm..."
  helm uninstall metallb -n metallb-system || true
fi

if command -v kubectl >/dev/null 2>&1; then
  fun "Removing metallb namespace (if present)..."
  kubectl delete namespace metallb-system || true
fi

if command -v cilium >/dev/null 2>&1; then
  fun "Uninstalling Cilium..."
  cilium uninstall || true
fi

fun "Running kubeadm reset (force)..."
sudo kubeadm reset --force || true

fun "Cleaning CNI configs..."
sudo rm -rf /etc/cni/net.d || true

# stop/disable services (ignore failures)
fun "Stopping kubelet & crio services..."
sudo systemctl stop kubelet || true
sudo systemctl disable kubelet || true

sudo systemctl stop crio || true
sudo systemctl disable crio || true

fun "Removing kubelet unit files and drop-ins from common locations..."
for dir in /lib/systemd/system /usr/lib/systemd/system /etc/systemd/system /run/systemd/system; do
  if [ -L "${dir}/kubelet.service" ] || [ -f "${dir}/kubelet.service" ]; then
    sudo rm -fv "${dir}/kubelet.service" || true
  fi
  if [ -d "${dir}/kubelet.service.d" ]; then
    sudo rm -fv "${dir}/kubelet.service.d/"* 2>/dev/null || true
    sudo rmdir -v "${dir}/kubelet.service.d" 2>/dev/null || true
  fi
done

# Additional known paths to clean
sudo rm -f /usr/lib/systemd/system/kubelet.service || true
sudo rm -f /usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf || true

fun "Reloading systemd to clear stale unit info..."
sudo systemctl daemon-reload || true
sudo systemctl reset-failed || true

fun "Removing binaries and user kube config..."
sudo rm -f /usr/local/bin/kubeadm || true
sudo rm -f /usr/local/bin/kubelet || true
sudo rm -f /usr/local/bin/cilium || true
sudo rm -rf "${HOME}/.kube" || true
sudo rm -f /etc/sysctl.d/*.conf || true

# unmount common runtime paths (ignore failures)
fun "Unmounting runtime paths..."
for path in \
    /opt/solo/weaver/sandbox/var/lib/containers/storage/overlay \
    /var/lib/kubelet \
    /var/run/cilium \
    /etc/kubernetes; do
  sudo umount -R "${path}" 2>/dev/null || true
done

# disable swap and kill API port listeners
fun "Disabling swap and freeing API port 6443..."
sudo swapoff -a || true
sudo lsof -t -i :6443 | xargs -r sudo kill -9 || true

# final daemon-reload to ensure systemd sees removals
sudo systemctl daemon-reload || true
sudo systemctl reset-failed || true

fun "Attempting to delete /opt/solo/weaver (best effort)..."
sudo rm -rf /opt/solo/weaver 2> failed-rm.txt || true

fun "Running final cleanup script on any remaining files..."
cat failed-rm.txt | ./test/scripts/cleanup.sh || true

printf "\n%s\n" "${_green}✅ Reset complete. May your nodes rise anew!${_reset}"
