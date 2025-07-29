# Production Workflow for Kubernetes Cluster Provisioning

This document outlines the sequence of workflows for provisioning a production-grade Kubernetes cluster using CRI-O as
the container runtime, Cilium as the CNI, and MetalLB for load balancing. The workflows are designed to be executed on a
clean Ubuntu 22.04 server.

# Workflows & Steps

***Note: The workflows and steps sequences are documented for reference only and the actual execution code must be
implemented
based on code snippet in `ubuntu-production.sh` bash script.***

**SetupDirectoryStructureWorkflow**: Set up required directories and file structure for provisioner.

- **SetupTempDirsStep**: Create working directories for temporary downloads and unpacking.
    - Create `/tmp/provisioner/downloads`.
    - Create `/tmp/provisioner/unpack`.
    - Create `/tmp/provisioner/backup`.
    - ...
- **SetupProvisionerDirsStep**: Create main provisioner directories for binaries, logs, and config.
    - Create `/opt/provisioner/bin`.
    - Create `/opt/provisioner/logs`.
    - Create `/opt/provisioner/config`.
    - ...
- **SetupSandboxDirsStep**: Create sandbox directories for binaries, configuration, runtime, and storage.
    - Create `/opt/sandbox/bin`.
    - Create `/opt/sandbox/config`.
    - Create `/opt/sandbox/run`.
    - Create `/opt/sandbox/storage`.
    - ...

**PrepareOperatingSystemWorkflow**: Prepare and configure the base system for Kubernetes installation.
- **UpdateOperatingSystemWorkflow**: Update and upgrade the operating system.
  - **UpdateOperatingSystemPackagesStep**: Update system packages.
      - Run
        - `apt update`.
      - Rollback: Nop. We cannot undo this, can we?
  - **UpgradeOperatingSystemPackagesStep**: Upgrade system packages.
      - Run 
        - `apt upgrade -y`.
      - Rollback: Nop. We cannot undo this, can we?
- **DisableSwapWorkflow**: Disable swap
    - **MakeBackupFileStep**: Backup original `/etc/fstab` if it exists.
      - Run:
        - If `/tmp/provisioner/backup/fstab.original` exists, do nothing.
        - If there is no `/tmp/provisioner/backup/fstab.original`, make a backup of original `/etc/fstab` in
        `/tmp/provisioner/backup/fstab.original`
      - Rollback: Nop. We don't want to delete the original backup file.
    - **DisableSwapStep**: Disable swap in the system.
      - Run 
        - Copy existing `/tmp/provisioner/backup/fstab.original` to `/etc/fstab`
        - `sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab`
        - `swapoff -a`.
      - Rollback:
        - Copy existing `/tmp/provisioner/backup/fstab.original` to `/etc/fstab`
        - `swapoff -a`.
- **CleanupContainerdWorkflow**: Clean up any existing containerd installations.
    - **RemovePackagesStep**: `containerd` - Remove any existing containerd installations.
      - Run:
        - `apt remove -y containerd`.
      - Rollback: Nop. We cannot undo this, can we?
    - **RemovePackagesStep**: `containerd.io` - Remove any existing containerd installations.
      - Run:
        - `apt remove -y containerd.io`.
      - Rollback: Nop. We cannot undo this, can we?

**LoadKernelModulesWorkflow**: Load necessary kernel modules for Kubernetes networking.

- **LoadOverlayModulesStep**: Load overlay filesystem modules for Kubernetes networking.
    - Run `modprobe overlay`.
    - Run `echo "overlay" | sudo tee /etc/modules-load.d/overlay.conf`
- **LoadBrNetfilterModulesStep**: Load bridge netfilter modules for Kubernetes networking.
    - Run `modprobe br_netfilter`.
    - Run `echo "br_netfilter" | sudo tee /etc/modules-load.d/br_netfilter.conf`

**InstallBasePackagesWorkflow**: Install required base packages for Kubernetes provisioning.

- **InstallIptablesStep**: Install iptables package.
    - Run `apt install -y iptables`.
- **InstallGpgStep**: Install gpg package.
    - Run `apt install -y gpg`.
- **InstallConntrackStep**: Install conntrack package.
    - Run `apt install -y conntrack`.
- **InstallSocatStep**: Install socat package.
    - Run `apt install -y socat`.
- **InstallEbtablesStep**: Install ebtables package.
    - Run `apt install -y ebtables`.
- **InstallNftablesStep**: Install nftables package.
    - Run `apt install -y nftables`.

**ConfigureNftablesWorkflow**: Enable and configure nftables service.

- **EnableNftablesServiceStep**: Enable nftables service.
    - Run `systemctl enable nftables`.
- **StartNftablesServiceStep**: Start nftables service.
    - Run `systemctl start nftables`.

**ConfigureSysctlWorkflow**: Enable and configure for Kubernetes provisioning.

- **CleanupSysctlStep**: Clean up old sysctl configuration files.
    - Run `sudo rm -f /etc/sysctl.d/15-network-performance.conf || true`
    - Run `sudo rm -f /etc/sysctl.d/15-k8s-networking.conf || true`
    - Run `sudo rm -f /etc/sysctl.d/15-inotify.conf || true`
- **Configure75K8sNetworkPerformanceStep**: Configure network performance settings.
    - Backup (if not already) `/etc/sysctl.d/75-k8s-networking.conf`.
    - Run `cat <<EOF | sudo tee /etc/sysctl.d/75-k8s-networking.conf
                  net.bridge.bridge-nf-call-iptables = 1
                  net.bridge.bridge-nf-call-ip6tables = 1
                  net.ipv4.ip_forward = 1
                  net.ipv6.conf.all.forwarding = 1
                  EOF`
- **Configure75NetworkPerformanceStep**: Configure network performance settings.
    - Backup (if not already) `/etc/sysctl.d/75-network-performance.conf`.
    - Run `cat <<EOF | sudo tee /etc/sysctl.d/75-network-performance.conf
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
              EOF`
- **Configure75KInotifyStep**: Configure network performance settings.
    - Backup (if not already) `/etc/sysctl.d/75-inotify.conf`.
    - Run `cat <<EOF | sudo tee /etc/sysctl.d/75-inotify.conf
              fs.inotify.max_user_watches = 524288
              fs.inotify.max_user_instances = 512
              EOF`
- **ApplySysctlStep**: Apply all sysctl settings.
    - Run `sysctl --system`.

**FetchPackagesWorkflow**: Download, verify, and extract required binaries for Kubernetes provisioning.

- **FetchCrioWorkflow**: Download, verify, and extract CRI-O binary.
    - **DownloadBinaryStep**: Download CRI-O binary.
    - **VerifyBinaryStep**: Verify CRI-O binary checksum.
    - **ExtractBinaryStep**: Unpack and extract CRI-O binary to `/opt/sandbox/bin`.
- **FetchKubeadmWorkflow**: Download, verify, and extract kubeadm binary.
    - **DownloadBinaryStep**: Download kubeadm binary.
    - **VerifyBinaryStep**: Verify kubeadm binary checksum.
    - **ExtractBinaryStep**: Unpack and extract kubeadm binary to `/opt/sandbox/bin`.
- **FetchKubeletWorkflow**: Download, verify, and extract kubelet binary.
    - **DownloadBinaryStep**: Download kubelet binary.
    - **VerifyBinaryStep**: Verify kubelet binary checksum.
    - **ExtractBinaryStep**: Unpack and extract kubelet binary to `/opt/sandbox/bin`.
- **FetchKubectlWorkflow**: Download, verify, and extract kubectl binary.
    - **DownloadBinaryStep**: Download kubectl binary.
    - **VerifyBinaryStep**: Verify kubectl binary checksum.
    - **ExtractBinaryStep**: Unpack and extract kubectl binary to `/opt/sandbox/bin`.
- **FetchK9sWorkflow**: Download, verify, and extract K9s binary.
    - **DownloadBinaryStep**: Download K9s binary.
    - **VerifyBinaryStep**: Verify K9s binary checksum.
    - **ExtractBinaryStep**: Unpack and extract K9s binary to `/opt/sandbox/bin`.
- **FetchHelmWorkflow**: Download, verify, and extract Helm binary.
    - **DownloadBinaryStep**: Download Helm binary.
    - **VerifyBinaryStep**: Verify Helm binary checksum.
    - **ExtractBinaryStep**: Unpack and extract Helm binary to `/opt/sandbox/bin`.
- **FetchCiliumCLIWorkflow**: Download, verify, and extract Cilium CLI binary.
    - **DownloadBinaryStep**: Download Cilium CLI binary.
    - **VerifyBinaryStep**: Verify Cilium CLI SHA256 checksum.
    - **ExtractBinaryStep**: Unpack and extract Cilium CLI binary to `/opt/sandbox/bin`.

**SetupServicesAndConfigWorkflow**: Configure systemd services and CRI-O/Kubernetes settings.

- **SetupSystemdServicesStep**: Copy and modify systemd service files for kubelet and CRI-O to use sandbox paths.
    - Copy original service files to `/etc/systemd/system/`.
    - Edit service files to point to sandbox binaries and config.
- **SetupCrioConfigStep**: Create and update CRI-O configuration files using dasel.
    - Use dasel to set required CRI-O config values.
- **SetupSystemdSymlinksStep**: Set up symlinks for systemd service files.
    - Create symlinks in `/etc/systemd/system/` for kubelet and CRI-O.

**SetPermissionsAndMountsWorkflow**: Set correct permissions and mount points for directories.

- **SetProvisionerOwnershipStep**: Set ownership of provisioner directories to the current user.
    - Run `sudo chown -R "${USER}:${GROUP}" "${PROVISIONER_HOME}"`.
- **SetSandboxOwnershipStep**: Set ownership of sandbox directories to root.
    - Run `sudo chown -R "root:root" "${SANDBOX_DIR}"`.
- **SetupBindMountsStep**: Set up bind mounts for `/etc/kubernetes` and `/var/lib/kubelet` to sandbox equivalents.
    - Run `sudo mkdir -p /etc/kubernetes /var/lib/kubelet`
    - Run `if ! grep -q "/etc/kubernetes" /etc/fstab; then
    echo "${SANDBOX_DIR}/etc/kubernetes /etc/kubernetes none bind,nofail 0 0" | sudo tee -a /etc/fstab >/dev/null
      fi`
    - Run `if ! grep -q "/var/lib/kubelet" /etc/fstab; then
    echo "${SANDBOX_DIR}/var/lib/kubelet /var/lib/kubelet none bind,nofail 0 0" | sudo tee -a /etc/fstab >/dev/null
    fi`
- **ApplyBindMountsStep**: Mount these directories.
    - Run `mount /etc/kubernetes`.
    - Run `mount /var/lib/kubelet`.

**ActivateCoreServicesWorkflow**: Enable and start core services (CRI-O, kubelet).

- **ReloadSystemdStep**: Reload systemd daemon.
    - Run `systemctl daemon-reload`.
- **EnableStartServicesStep**: Enable and start CRI-O and kubelet services.
    - Run `systemctl enable crio`.
    - Run `systemctl start crio`.
    - Run `systemctl enable kubelet`.
    - Run `systemctl start kubelet`.

**InitializeKubernetesClusterWorkflow**: Initialize the Kubernetes cluster.

- **ResetKubeadmStep**: Reset any previous kubeadm configuration.
    - Run `kubeadm reset -f`.
- **CleanupK8sDataStep**: Clean up old Kubernetes and etcd data.
    - Remove `/etc/kubernetes/*`.
    - Remove `/var/lib/etcd/*`.
- **GenerateKubeadmConfigStep**: Generate kubeadm configuration file with bootstrap token and machine IP.
    - Create `kubeadm-config.yaml` with required settings.
- **InitKubeadmStep**: Initialize the Kubernetes cluster with kubeadm.
    - Run `kubeadm init --config=kubeadm-config.yaml`.
- **SetupKubeconfigStep**: Set up kubeconfig for the current user.
    - Copy `/etc/kubernetes/admin.conf` to `$HOME/.kube/config`.

**SetupCiliumCNIWorkflow**: Install and configure CNI (Cilium).

- **GenerateCiliumConfigStep**: Generate and apply Cilium configuration.
    - Create `cilium-values.yaml` with required settings.
- **InstallCiliumStep**: Install Cilium CNI.
    - Run `cilium install --values cilium-values.yaml`.
- **RestartCNIServiceStep**: Restart kubelet and CRI-O to ensure CNI is ready.
    - Run `systemctl restart kubelet`.
    - Run `systemctl restart crio`.
- **CheckCiliumReadyStep**: Wait for Cilium to be ready.
    - Check Cilium pod status with `kubectl`.

**SetupMetalLBLoadBalancerWorkflow**: Install and configure load balancer (MetalLB).

- **AddHelmRepoStep**: Add Helm repo for MetalLB.
    - Run `helm repo add metallb https://metallb.github.io/metallb`.
    - Run `helm repo update`.
- **InstallMetalLBStep**: Install MetalLB via Helm.
    - Run `helm install metallb metallb/metallb --namespace metallb-system --create-namespace`.
- **ApplyMetalLBConfigStep**: Apply MetalLB configuration for IP pools and L2 advertisement.
    - Create and apply `metallb-config.yaml` with `kubectl apply -f`.