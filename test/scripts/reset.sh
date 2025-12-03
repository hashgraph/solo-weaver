#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
helm uninstall metallb -n metallb-system || true
kubectl delete namespace metallb-system || true
cilium uninstall || true
sudo kubeadm reset --force || true
sudo rm -rf /etc/cni/net.d || true
sudo systemctl stop kubelet || true
sudo systemctl disable kubelet || true
sudo rm -f /usr/lib/systemd/system/kubelet.service
sudo systemctl stop crio || true
sudo systemctl disable crio || true
sudo rm -f /usr/lib/systemd/system/crio.service
sudo systemctl daemon-reload || true
sudo rm -f /usr/local/bin/kubeadm || true
sudo rm -f /usr/local/bin/kubelet || true
sudo rm -f /usr/local/bin/cilium || true
sudo rm -rf $HOME/.kube || true
sudo rm -f /etc/sysctl.d/*.conf || true
for path in \
    /opt/weaver/sandbox/var/run \
    /opt/weaver/sandbox/var/lib/containers/storage/overlay; do
  sudo umount -R $path || true
done
sudo umount -R /var/lib/kubelet || true
sudo umount -R /var/run/cilium || true
sudo umount -R /etc/kubernetes || true
sudo swapoff -a || true
sudo lsof -t -i :6443 | xargs -r sudo kill -9 || true
sudo rm -rf /opt/weaver || true
