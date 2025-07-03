#!/usr/bin/env bash
set -ex

OS="$(uname -s)"
OS="${OS,,}"
ARCH="$(dpkg --print-architecture)"
readonly OS ARCH

USER="$(id -un)"
GROUP="$(id -gn)"
readonly USER GROUP

readonly PROVISIONER_HOME="/opt/solo/provisioner"
readonly SANDBOX_DIR="${PROVISIONER_HOME}/sandbox"
readonly SANDBOX_BIN="${SANDBOX_DIR}/bin"
readonly SANDBOX_LOCAL_BIN="${SANDBOX_DIR}/usr/local/bin"

readonly CRIO_VERSION="1.33.2"
readonly KUBERNETES_VERSION="1.33.2"
readonly KREL_VERSION="v0.18.0"
readonly K9S_VERSION="0.50.6"
readonly HELM_VERSION="3.18.3"
readonly CILIUM_CLI_VERSION="0.18.5"
readonly CILIUM_VERSION="1.17.5"
readonly METALLB_VERSION="0.15.2"
readonly DASEL_VERSION="2.8.1"

# Update System Packages
sudo apt update && sudo apt upgrade -y

# Disable Swap
sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab
sudo swapoff -a

# Remove existing containerd
sudo apt remove -y containerd containerd.io || true

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
mkdir -p /tmp/provisioner/utils
mkdir -p /tmp/provisioner/cri-o/unpack
mkdir -p /tmp/provisioner/kubernetes
mkdir -p /tmp/provisioner/cilium

# Download Components
pushd "/tmp/provisioner/utils" >/dev/null 2>&1 || true
curl -sSLo "dasel_${OS}_${ARCH}" "https://github.com/TomWright/dasel/releases/download/v${DASEL_VERSION}/dasel_${OS}_${ARCH}"
popd >/dev/null 2>&1 || true

pushd "/tmp/provisioner/cri-o" >/dev/null 2>&1 || true
curl -sSLo "cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz" "https://storage.googleapis.com/cri-o/artifacts/cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz"
popd >/dev/null 2>&1 || true

pushd "/tmp/provisioner/kubernetes" >/dev/null 2>&1 || true
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

# Setup Production Provisioner Folders
sudo mkdir -p ${PROVISIONER_HOME}
sudo mkdir -p ${PROVISIONER_HOME}/bin
sudo mkdir -p ${PROVISIONER_HOME}/logs
sudo mkdir -p ${PROVISIONER_HOME}/config

# Setup Provisioner Sandbox
sudo mkdir -p ${SANDBOX_DIR}
sudo mkdir -p ${SANDBOX_DIR}/bin
sudo mkdir -p ${SANDBOX_DIR}/etc/crio
sudo mkdir -p ${SANDBOX_DIR}/etc/default
sudo mkdir -p ${SANDBOX_DIR}/etc/provisioner
sudo mkdir -p ${SANDBOX_DIR}/etc/containers/registries.conf.d
sudo mkdir -p ${SANDBOX_DIR}/etc/cni/net.d
sudo mkdir -p ${SANDBOX_DIR}/etc/kubernetes/pki
sudo mkdir -p ${SANDBOX_DIR}/var/lib/containerd
sudo mkdir -p ${SANDBOX_DIR}/var/lib/etcd
sudo mkdir -p ${SANDBOX_DIR}/var/lib/kubelet
sudo mkdir -p ${SANDBOX_DIR}/var/run
sudo mkdir -p ${SANDBOX_DIR}/var/run/cilium
sudo mkdir -p ${SANDBOX_DIR}/usr/libexec/crio
sudo mkdir -p ${SANDBOX_DIR}/usr/lib/systemd/system/kubelet.service.d
sudo mkdir -p ${SANDBOX_DIR}/usr/local/bin
sudo mkdir -p ${SANDBOX_DIR}/usr/local/share/man
sudo mkdir -p ${SANDBOX_DIR}/usr/local/share/oci-umount/oci-umount.d
sudo mkdir -p ${SANDBOX_DIR}/usr/local/share/bash-completion/completions
sudo mkdir -p ${SANDBOX_DIR}/usr/local/share/fish/completions
sudo mkdir -p ${SANDBOX_DIR}/usr/local/share/zsh/site-functions
sudo mkdir -p ${SANDBOX_DIR}/opt/cni/bin

# Setup Ownership and Permissions
sudo chown -R "${USER}:${GROUP}" "${PROVISIONER_HOME}"
sudo chown -R "root:root" "${SANDBOX_DIR}"

# Setup Bind Mounts
sudo mkdir -p /etc/kubernetes /var/lib/kubelet

if ! grep -q "/etc/kubernetes" /etc/fstab; then
  echo "${SANDBOX_DIR}/etc/kubernetes /etc/kubernetes none bind,nofail 0 0" | sudo tee -a /etc/fstab >/dev/null
fi

if ! grep -q "/var/lib/kubelet" /etc/fstab; then
  echo "${SANDBOX_DIR}/var/lib/kubelet /var/lib/kubelet none bind,nofail 0 0" | sudo tee -a /etc/fstab >/dev/null
fi

sudo mount /etc/kubernetes
sudo mount /var/lib/kubelet

# Install CRI-O
sudo tar -C "/tmp/provisioner/cri-o/unpack" -zxvf "/tmp/provisioner/cri-o/cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz"
pushd "/tmp/provisioner/cri-o/unpack/cri-o" >/dev/null 2>&1 || true
DESTDIR="${SANDBOX_DIR}" SYSTEMDDIR="/usr/lib/systemd/system" sudo -E "$(command -v bash)" ./install
popd >/dev/null 2>&1 || true

# Install Kubernetes
sudo install -m 755 "/tmp/provisioner/kubernetes/kubeadm" "${SANDBOX_BIN}/kubeadm"
sudo install -m 755 "/tmp/provisioner/kubernetes/kubelet" "${SANDBOX_BIN}/kubelet"
sudo install -m 755 "/tmp/provisioner/kubernetes/kubectl" "${SANDBOX_BIN}/kubectl"

sudo mkdir -p ${SANDBOX_DIR}/usr/lib/systemd/system/kubelet.service.d
sudo cp "/tmp/provisioner/kubernetes/kubelet.service" "${SANDBOX_DIR}/usr/lib/systemd/system/kubelet.service"
sudo cp "/tmp/provisioner/kubernetes/10-kubeadm.conf" "${SANDBOX_DIR}/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf"

# Install K9s
sudo tar -C ${SANDBOX_BIN} -zxvf "/tmp/provisioner/kubernetes/k9s_${OS^}_${ARCH}.tar.gz" k9s

# Install Helm
sudo tar -C ${SANDBOX_BIN} --strip-components=1 -zxvf "/tmp/provisioner/kubernetes/helm-v${HELM_VERSION}-${OS}-${ARCH}.tar.gz" "${OS}-${ARCH}/helm"

# Install Cilium
sudo tar -C ${SANDBOX_BIN} -zxvf "/tmp/provisioner/cilium/cilium-${OS}-${ARCH}.tar.gz"

# Setup Systemd Service SymLinks
sudo ln -sf ${SANDBOX_DIR}/usr/lib/systemd/system/kubelet.service /usr/lib/systemd/system/kubelet.service
sudo ln -sf ${SANDBOX_DIR}/usr/lib/systemd/system/kubelet.service.d /usr/lib/systemd/system/kubelet.service.d
sudo ln -sf ${SANDBOX_DIR}/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service

# Enable and Start Services
sudo systemctl daemon-reload
sudo systemctl enable crio kubelet
sudo systemctl start crio kubelet

# Torch prior KubeADM Configuration
#sudo ${SANDBOX_BIN}/kubeadm reset --force || true
#sudo rm -rf ${SANDBOX_DIR}/etc/kubernetes/* ${SANDBOX_DIR}/etc/cni/net.d/* ${SANDBOX_DIR}/var/lib/etcd/* || true

# Setup KubeADM Configuration
kube_bootstrap_token="$(${SANDBOX_BIN}/kubeadm token generate)"
machine_ip="$(ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}')"

cat <<EOF | sudo tee ${SANDBOX_DIR}/etc/provisioner/kubeadm-init.yaml >/dev/null
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
certificatesDir: ${SANDBOX_DIR}/etc/kubernetes/pki
caCertificateValidityPeriod: 87600h0m0s
certificateValidityPeriod: 8760h0m0s
encryptionAlgorithm: RSA-2048
clusterName: k8s.main.gcp
etcd:
  local:
    dataDir: ${SANDBOX_DIR}/var/lib/etcd
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
sudo ${SANDBOX_BIN}/kubeadm init --upload-certs --config ${SANDBOX_DIR}/etc/provisioner/kubeadm-init.yaml
mkdir -p "${HOME}/.kube"
sudo cp -f /etc/kubernetes/admin.conf "${HOME}/.kube/config"
sudo chown "${USER}:${GROUP}" "${HOME}/.kube/config"

# Configure Cilium
cat <<EOF | sudo tee ${SANDBOX_DIR}/etc/provisioner/cilium-config.yaml >/dev/null
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

# CNI Configuration
cni:
  cniBinPath: ${SANDBOX_DIR}/opt/cni/bin
  cniConfPath: ${SANDBOX_DIR}/etc/cni/net.d

# DaemonSet Configuration
daemon:
  runPath: ${SANDBOX_DIR}/var/run/cilium

EOF

# Install Cilium CNI
${SANDBOX_BIN}/cilium install --version "${CILIUM_VERSION}" --values ${SANDBOX_DIR}/etc/provisioner/cilium-config.yaml

# Restart Container and Kubelet (fix for cilium CNI not initializing - CNI not ready error)
sudo sysctl --system >/dev/null
sudo systemctl restart containerd kubelet

${SANDBOX_BIN}/cilium status --wait

# Install MetalLB
${SANDBOX_BIN}/helm repo add metallb https://metallb.github.io/metallb
${SANDBOX_BIN}/helm install metallb metallb/metallb --version ${METALLB_VERSION} \
  --set speaker.frr.enabled=false \
  --namespace metallb-system --create-namespace --atomic --wait

# Deploy MetalLB Configuration
cat <<EOF | ${SANDBOX_BIN}/kubectl apply -f -
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



