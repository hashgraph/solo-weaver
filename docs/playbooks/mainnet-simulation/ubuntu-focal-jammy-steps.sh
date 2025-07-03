#!/usr/bin/env bash
set -x

OS="$(uname -s)"
OS="${OS,,}"
ARCH="$(dpkg --print-architecture)"
readonly OS ARCH

USER="$(id -un)"
GROUP="$(id -gn)"
readonly USER GROUP

readonly CONTAINERD_VERSION="1.7.26"
readonly RUNC_VERSION="1.2.5"
readonly CNI_PLUGINS_VERSION="1.6.2"
readonly CRICTL_VERSION="1.32.0"
readonly KUBERNETES_VERSION="1.32.3"
readonly KREL_VERSION="v0.18.0"
readonly K9S_VERSION="0.40.8"
readonly HELM_VERSION="3.17.1"
readonly CILIUM_CLI_VERSION="0.18.2"
readonly CILIUM_VERSION="1.17.1"
readonly METALLB_VERSION="0.14.9"

# Update System Packages
sudo apt update && sudo apt upgrade -y

# Disable Swap
sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab
sudo swapoff -a

# Remove existing containerd
sudo apt remove -y containerd containerd.io

# Install iptables
sudo apt install -y iptables

# Install gpg package (if required)
sudo apt install -y gpg

# Install Conntrack, EBTables, SoCat, and NFTables
sudo apt install -y conntrack socat ebtables nftables
sudo apt autoremove -y

# Enable nftables service
sudo systemctl enable nftables
sudo systemctl start nftables

# Install Kernel Modules
sudo modprobe overlay
sudo modprobe br_netfilter
echo "overlay" | sudo tee /etc/modules-load.d/overlay.conf
echo "br_netfilter" | sudo tee /etc/modules-load.d/br_netfilter.conf

sudo rm -f /etc/sysctl.d/15-network-performance.conf || true
sudo rm -f /etc/sysctl.d/15-k8s-networking.conf || true
sudo rm -f /etc/sysctl.d/15-inotify.conf || true

# Configure System Control Settings
cat <<EOF | sudo tee /etc/sysctl.d/75-k8s-networking.conf
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
EOF

cat <<EOF | sudo tee /etc/sysctl.d/75-network-performance.conf
net.core.rmem_default = 31457280
net.core.wmem_default = 31457280
net.core.rmem_max = 33554432
net.core.wmem_max = 33554432
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 65535
net.core.optmem_max = 25165824
net.ipv4.tcp_synack_retries = 2
net.ipv4.tcp_rmem = 8192 65536 33554432
net.ipv4.tcp_mem = 786432 1048576 26777216
net.ipv4.udp_mem = 65536 131072 262144
net.ipv4.udp_rmem_min = 16384
net.ipv4.tcp_wmem = 8192 65536 33554432
net.ipv4.udp_wmem_min = 16384
net.ipv4.tcp_max_tw_buckets = 1440000
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_rfc1337 = 1
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_intvl = 15
fs.file-max = 2097152
vm.swappiness = 10
vm.dirty_ratio = 60
vm.dirty_background_ratio = 2
EOF

cat <<EOF | sudo tee /etc/sysctl.d/75-inotify.conf
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 512
EOF

sudo sysctl --system >/dev/null

# Configure cgroupv2 of focal
#sudo sed -i 's/^GRUB_CMDLINE_LINUX="\(.*\)"$/GRUB_CMDLINE_LINUX="systemd.unified_cgroup_hierarchy=1"/' /etc/default/grub
#sudo update-grub

# Setup working directories
mkdir -p /tmp/provisioner/containerd
mkdir -p /tmp/provisioner/runc
mkdir -p /tmp/provisioner/cni
mkdir -p /tmp/provisioner/kubernetes
mkdir -p /tmp/provisioner/cilium

# Cleanup Kube Components in Wrong Directory
rm -f /usr/local/bin/kubeadm /usr/local/bin/kubelet /usr/local/bin/kubectl || true

# Download Components
pushd "/tmp/provisioner/runc" >/dev/null 2>&1 || true
curl -sSLo "runc.${ARCH}" "https://github.com/opencontainers/runc/releases/download/v${RUNC_VERSION}/runc.${ARCH}"
curl -sSLo "runc.${ARCH}.asc" "https://github.com/opencontainers/runc/releases/download/v${RUNC_VERSION}/runc.${ARCH}.asc"
gpg --verify "runc.${ARCH}.asc" "runc.${ARCH}"
popd >/dev/null 2>&1 || true

pushd "/tmp/provisioner/containerd" >/dev/null 2>&1 || true
curl -sSLo "containerd-${CONTAINERD_VERSION}-${OS}-${ARCH}.tar.gz" "https://github.com/containerd/containerd/releases/download/v${CONTAINERD_VERSION}/containerd-${CONTAINERD_VERSION}-${OS}-${ARCH}.tar.gz"
curl -sSLo "containerd-${CONTAINERD_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum" "https://github.com/containerd/containerd/releases/download/v${CONTAINERD_VERSION}/containerd-${CONTAINERD_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum"
sha256sum -c "containerd-${CONTAINERD_VERSION}-${OS}-${ARCH}.tar.gz.sha256sum"
curl -sSLo containerd.service https://raw.githubusercontent.com/containerd/containerd/main/containerd.service
popd >/dev/null 2>&1 || true

pushd "/tmp/provisioner/cni" >/dev/null 2>&1 || true
curl -sSLo "cni-plugins-${OS}-${ARCH}-v${CNI_PLUGINS_VERSION}.tgz" "https://github.com/containernetworking/plugins/releases/download/v${CNI_PLUGINS_VERSION}/cni-plugins-${OS}-${ARCH}-v${CNI_PLUGINS_VERSION}.tgz"
curl -sSLo "cni-plugins-${OS}-${ARCH}-v${CNI_PLUGINS_VERSION}.tgz.sha256" "https://github.com/containernetworking/plugins/releases/download/v${CNI_PLUGINS_VERSION}/cni-plugins-${OS}-${ARCH}-v${CNI_PLUGINS_VERSION}.tgz.sha256"
sha256sum -c "cni-plugins-${OS}-${ARCH}-v${CNI_PLUGINS_VERSION}.tgz.sha256"
popd >/dev/null 2>&1 || true

pushd "/tmp/provisioner/kubernetes" >/dev/null 2>&1 || true
curl -sSLo "crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz" "https://github.com/kubernetes-sigs/cri-tools/releases/download/v${CRICTL_VERSION}/crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz"
curl -sSLo "crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz.sha256" "https://github.com/kubernetes-sigs/cri-tools/releases/download/v${CRICTL_VERSION}/crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz.sha256"
printf "%s %s" "$(tr -d '[:space:]' < "crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz.sha256" )" "crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz" | sha256sum -c -

curl -sSLo kubeadm "https://dl.k8s.io/release/v${KUBERNETES_VERSION}/bin/${OS}/${ARCH}/kubeadm"
curl -sSLo kubelet "https://dl.k8s.io/release/v${KUBERNETES_VERSION}/bin/${OS}/${ARCH}/kubelet"
curl -sSLo kubectl "https://dl.k8s.io/release/v${KUBERNETES_VERSION}/bin/${OS}/${ARCH}/kubectl"
sudo chmod +x kubeadm kubelet kubectl
curl -sSLo kubelet.service "https://raw.githubusercontent.com/kubernetes/release/${KREL_VERSION}/cmd/krel/templates/latest/kubelet/kubelet.service"
curl -sSLo 10-kubeadm.conf "https://raw.githubusercontent.com/kubernetes/release/${KREL_VERSION}/cmd/krel/templates/latest/kubeadm/10-kubeadm.conf"
curl -sSLo "k9s_${OS^}_${ARCH}.tar.gz" "https://github.com/derailed/k9s/releases/download/v${K9S_VERSION}/k9s_${OS^}_${ARCH}.tar.gz"
curl -sSLo "helm-v${HELM_VERSION}-${OS}-${ARCH}.tar.gz" "https://get.helm.sh/helm-v${HELM_VERSION}-${OS}-${ARCH}.tar.gz"
popd >/dev/null 2>&1 || true

pushd "/tmp/provisioner/cilium" >/dev/null 2>&1 || true
curl -sSLo "cilium-${OS}-${ARCH}.tar.gz" "https://github.com/cilium/cilium-cli/releases/download/v${CILIUM_CLI_VERSION}/cilium-${OS}-${ARCH}.tar.gz"
curl -sSLo "cilium-${OS}-${ARCH}.tar.gz.sha256sum" "https://github.com/cilium/cilium-cli/releases/download/v${CILIUM_CLI_VERSION}/cilium-${OS}-${ARCH}.tar.gz.sha256sum"
sha256sum -c "cilium-${OS}-${ARCH}.tar.gz.sha256sum"
popd >/dev/null 2>&1 || true

# Install Runc
sudo install -m 755 "/tmp/provisioner/runc/runc.${ARCH}" "/usr/local/sbin/runc"

# Install ContainerD
sudo tar -C /usr/local -zxvf "/tmp/provisioner/containerd/containerd-${CONTAINERD_VERSION}-${OS}-${ARCH}.tar.gz"

# Install CNI Plugins
sudo mkdir -p /opt/cni/bin
sudo tar -C /opt/cni/bin -zxvf "/tmp/provisioner/cni/cni-plugins-${OS}-${ARCH}-v${CNI_PLUGINS_VERSION}.tgz"

# Install CRICTL
sudo tar -C /usr/local/bin -zxvf "/tmp/provisioner/kubernetes/crictl-v${CRICTL_VERSION}-${OS}-${ARCH}.tar.gz"

# Install Kubernetes
sudo install -m 755 "/tmp/provisioner/kubernetes/kubeadm" "/usr/bin/kubeadm"
sudo install -m 755 "/tmp/provisioner/kubernetes/kubelet" "/usr/bin/kubelet"
sudo install -m 755 "/tmp/provisioner/kubernetes/kubectl" "/usr/bin/kubectl"

sudo mkdir -p /usr/lib/systemd/system/kubelet.service.d
sudo cp "/tmp/provisioner/kubernetes/kubelet.service" "/usr/lib/systemd/system/kubelet.service"
sudo cp "/tmp/provisioner/kubernetes/10-kubeadm.conf" "/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf"

# Install K9s
sudo tar -C /usr/local/bin -zxvf "/tmp/provisioner/kubernetes/k9s_${OS^}_${ARCH}.tar.gz" k9s

# Install Helm
sudo tar -C /usr/local/bin --strip-components=1 -zxvf "/tmp/provisioner/kubernetes/helm-v${HELM_VERSION}-${OS}-${ARCH}.tar.gz" "${OS}-${ARCH}/helm"

# Install Cilium
sudo tar -C /usr/local/bin -zxvf "/tmp/provisioner/cilium/cilium-${OS}-${ARCH}.tar.gz"

# Install ContainerD Configuration
sudo mkdir -p /etc/containerd
sudo cp "/tmp/provisioner/containerd/containerd.service" "/usr/lib/systemd/system/containerd.service"
sudo containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
sudo sed -i.bak 's/^\(.*\)SystemdCgroup =.*$/\1SystemdCgroup = true/' /etc/containerd/config.toml

# Enable and Start Services
sudo systemctl daemon-reload
sudo systemctl enable containerd kubelet
sudo systemctl start containerd kubelet

# Torch prior KubeADM Configuration
sudo kubeadm reset --force || true
sudo rm -rf /etc/kubernetes /etc/cni/net.d /var/lib/etcd || true

# Setup KubeADM Configuration
kube_bootstrap_token="$(kubeadm token generate)"
machine_ip="$(ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}')"

cat <<EOF | sudo tee /tmp/provisioner/kubernetes/kubeadm-init.yaml >/dev/null
apiVersion: kubeadm.k8s.io/v1beta4
kind: InitConfiguration
bootstrapTokens:
  - groups:
    - system:bootstrappers:kubeadm:default-node-token
    token: ${kube_bootstrap_token}
    ttl: 720h0m0s
    usages:
      - signing
      - authentication
localAPIEndpoint:
  advertiseAddress: ${machine_ip}
  bindPort: 6443
nodeRegistration:
  imagePullPolicy: IfNotPresent
  imagePullSerial: true
  name: $(hostname)
  taints:
    - key: "node.cilium.io/agent-not-ready"
      value: "true"
      effect: "NoExecute"
  kubeletExtraArgs:
    - name: node-ip
      value: ${machine_ip}
skipPhases:
  - addon/kube-proxy
timeouts:
  controlPlaneComponentHealthCheck: 4m0s
  discovery: 5m0s
  etcdAPICall: 2m0s
  kubeletHealthCheck: 4m0s
  kubernetesAPICall: 1m0s
  tlsBootstrap: 5m0s
  upgradeManifests: 5m0s
---
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
controlPlaneEndpoint: "${machine_ip}:6443"
certificatesDir: /etc/kubernetes/pki
caCertificateValidityPeriod: 87600h0m0s
certificateValidityPeriod: 8760h0m0s
encryptionAlgorithm: RSA-2048
clusterName: k8s.main.gcp
etcd:
  local:
    dataDir: /var/lib/etcd
imageRepository: registry.k8s.io
kubernetesVersion: ${KUBERNETES_VERSION}
networking:
  dnsDomain: cluster.local
  serviceSubnet: 10.0.0.0/14
  podSubnet: 10.4.0.0/14
controllerManager:
  extraArgs:
    - name: node-cidr-mask-size-ipv4
      value: "24"
EOF

# Stop on failure past this point
set -eo pipefail

# Initialize Kubernetes Cluster
sudo kubeadm init --upload-certs --config /tmp/provisioner/kubernetes/kubeadm-init.yaml
mkdir -p "${HOME}/.kube"
sudo cp -f /etc/kubernetes/admin.conf "${HOME}/.kube/config"
sudo chown "${USER}:${GROUP}" "${HOME}/.kube/config"

# Configure Cilium
cat <<EOF | sudo tee /tmp/provisioner/cilium/cilium-config.yaml >/dev/null
# StepSecurity Required Features
extraArgs:
  - --tofqdns-dns-reject-response-code=nameError

# Hubble Support
hubble:
  relay:
    enabled: true
  ui:
    enabled: true

# KubeProxy Replacement Config
kubeProxyReplacement: true
k8sServiceHost: ${machine_ip}
k8sServicePort: 6443

# IP Version Support
ipam:
  mode: "kubernetes"
k8s:
  requireIPv4PodCIDR: true
  requireIPv6PodCIDR: false
ipv4:
  enabled: true
ipv6:
  enabled: false

# Routing Configuration
routingMode: native
autoDirectNodeRoutes: true
#ipv4NativeRoutingCIDR: 10.128.0.0/20

# Load Balancer Configuration
loadBalancer:
  mode: dsr
  dsrDispatch: opt
  algorithm: maglev
  acceleration: "best-effort"
  l7:
    backend: disabled

nodePort:
  enabled: true

hostPort:
  enabled: true

# BPF & IP Masquerading Support
ipMasqAgent:
  enabled: true
  config:
    nonMasqueradeCIDRs: []
bpf:
  masquerade: true
  hostLegacyRouting: false
  lbExternalClusterIP: true
  preallocateMaps: true

# Envoy DaemonSet Support
envoy:
  enabled: false

# BGP Control Plane
bgpControlPlane:
  enabled: false

# L2 Announcements
l2announcements:
  enabled: false
k8sClientRateLimit:
  qps: 100
  burst: 150

EOF

# Install Cilium CNI
cilium install --version "${CILIUM_VERSION}" --values /tmp/provisioner/cilium/cilium-config.yaml

# Restart Container and Kubelet (fix for cilium CNI not initializing - CNI not ready error)
sudo sysctl --system >/dev/null
sudo systemctl restart containerd kubelet

cilium status --wait

# Install MetalLB
helm repo add metallb https://metallb.github.io/metallb
helm install metallb metallb/metallb --version ${METALLB_VERSION} \
  --set speaker.frr.enabled=false \
  --namespace metallb-system --create-namespace --atomic --wait

# Deploy MetalLB Configuration
cat <<EOF | kubectl apply -f -
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: private-address-pool
  namespace: metallb-system
spec:
  addresses:
    - 192.168.99.0/24
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: public-address-pool
  namespace: metallb-system
spec:
  addresses:
    - ${machine_ip}/32
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: primary-l2-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
  - private-address-pool
  - public-address-pool
EOF


