package workflows

import (
	"context"
	"fmt"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"os"
	"runtime"
	"testing"
)

// testHelper mimics the bash script commands to help us create a test scenario for the
// actual step implementation and testing. This only assumes a debian or ubuntu OS
type testHelper struct {
	os                 string
	architecture       string
	uid                int
	gid                int
	provisionerHomeDir string
	sandboxDir         string
	sandboxBinDir      string
	sandboxLocalBinDir string
	crioVersion        string
	kubernetesVersion  string
	krelVersion        string
	k9sVersion         string
	helmVersion        string
	ciliumCliVersion   string
	ciliumVersion      string
	metallbVersion     string
	daselVersion       string
}

func newTestHelper() *testHelper {
	return &testHelper{
		os:                 runtime.GOOS,
		architecture:       runtime.GOARCH,
		uid:                os.Getuid(),
		gid:                os.Getgid(),
		provisionerHomeDir: core.ProvisionerHomeDir,
		sandboxDir:         core.SandboxDir,
		sandboxBinDir:      core.SandboxBinDir,
		sandboxLocalBinDir: core.SandboxLocalBinDir,
		crioVersion:        "1.33.4",
		kubernetesVersion:  "1.33.4",
		krelVersion:        "v0.18.0",
		k9sVersion:         "0.50.9",
		helmVersion:        "3.18.6",
		ciliumCliVersion:   "0.18.7",
		ciliumVersion:      "1.18.1",
		metallbVersion:     "0.15.2",
		daselVersion:       "1.30.0",
	}
}

func (tm *testHelper) UpdateOS() automa.Builder {
	return automa_steps.NewBashScriptStep("update-os", []string{
		"sudo apt-get update -y",
	}, "")
}

func (tm *testHelper) DisableSwap() automa.Builder {
	return automa_steps.NewBashScriptStep("disable-swap", []string{
		`sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab`,
		`sudo swapoff -a`,
	}, "")
}

func (tm *testHelper) RemoveContainerd() automa.Builder {
	return automa_steps.NewBashScriptStep("remove-containerd", []string{
		"sudo apt-get remove -y containerd containerd.io || true",
	}, "")
}

func (tm *testHelper) InstallIPTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install-iptables", []string{
		"sudo apt-get install -y iptables",
	}, "")
}

func (tm *testHelper) InstallGnupg2() automa.Builder {
	return automa_steps.NewBashScriptStep("install-gnupg2", []string{
		"sudo apt-get install -y gnupg2",
	}, "")
}

func (tm *testHelper) InstallConntrack() automa.Builder {
	return automa_steps.NewBashScriptStep("install-conntrack", []string{
		"sudo apt-get install -y conntrack",
	}, "")
}

func (tm *testHelper) InstallSoCat() automa.Builder {
	return automa_steps.NewBashScriptStep("install-socat", []string{
		"sudo apt-get install -y socat",
	}, "")
}

func (tm *testHelper) InstallEBTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install-ebtables", []string{
		"sudo apt-get install -y ebtables",
	}, "")
}

func (tm *testHelper) InstallNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install-nftables", []string{
		"sudo apt-get install -y nftables",
	}, "")
}

// Enable nftables service
func (tm *testHelper) EnableAndStartNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep("enable-start-nftables", []string{
		"sudo systemctl enable nftables",
		"sudo systemctl start nftables",
	}, "")
}

func (tm *testHelper) AutoRemovePackages() automa.Builder {
	return automa_steps.NewBashScriptStep("auto-remove-packages", []string{
		"sudo apt-get autoremove -y",
	}, "")
}

func (tm *testHelper) InstallKernelModules() automa.Builder {
	return automa_steps.NewBashScriptStep("install-kernel-modules", []string{
		"sudo modprobe overlay",
		"sudo modprobe br_netfilter",
		`echo 'overlay' | sudo tee /etc/modules-load.d/overlay.conf`,
		`echo 'br_netfilter' | sudo tee /etc/modules-load.d/br_netfilter.conf`,
	}, "")
}

func (tm *testHelper) ConfigureKernelModules() automa.Builder {
	return automa.NewWorkFlowBuilder("-kernel-modules").Steps(
		automa_steps.NewBashScriptStep("cleanup-systclt-configs", []string{
			`sudo rm -f /etc/sysctl.d/15-network-performance.conf || true`,
			`sudo rm -f /etc/sysctl.d/15-k8s-networking.conf || true`,
			`sudo rm -f /etc/sysctl.d/15-inotify.conf || true`,
		}, ""),
		automa_steps.NewBashScriptStep("configure-kernel-modules", []string{
			`echo 'net.bridge.bridge-nf-call-iptables = 1' | sudo tee /etc/sysctl.d/99-kubernetes-cri.conf`,
			`echo 'net.ipv4.ip_forward = 1' | sudo tee -a /etc/sysctl.d/99-kubernetes-cri.conf`,
			`echo 'net.bridge.bridge-nf-call-ip6tables = 1' | sudo tee -a /etc/sysctl.d/99-kubernetes-cri.conf`,
			"sudo sysctl --system >/dev/null",
		}, ""),
		automa_steps.NewBashScriptStep("create-k8s-networking-config", []string{
			`cat <<EOF | sudo tee /etc/sysctl.d/75-k8s-networking.conf
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
EOF`,
		}, ""),
		automa_steps.NewBashScriptStep("create-network-performance-config", []string{
			`cat <<EOF | sudo tee /etc/sysctl.d/75-network-performance.conf
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
EOF`,
		}, ""),
		automa_steps.NewBashScriptStep("create-inotify-config", []string{
			`cat <<EOF | sudo tee /etc/sysctl.d/75-inotify.conf
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 512
EOF`,
		}, ""),
		automa_steps.NewBashScriptStep("reload-sysctl", []string{
			"sudo sysctl --system >/dev/null",
		}, ""),
	)
}

func (tm *testHelper) setupWorkingDirectories() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-working-dirs", []string{
		"mkdir -p " + tm.provisionerHomeDir + "/utils",
		"mkdir -p " + tm.provisionerHomeDir + "/crio/unpack",
		"mkdir -p " + tm.provisionerHomeDir + "/kubernetes",
		"mkdir -p " + tm.provisionerHomeDir + "/cilium",
	}, "")
}

// Download dasel
func (tm *testHelper) DownloadDasel() automa.Builder {
	daselFile := "dasel_" + tm.os + "_" + tm.architecture
	return automa_steps.NewBashScriptStep("download-dasel", []string{
		fmt.Sprintf("pushd %s/utils >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo %s https://github.com/TomWright/dasel/releases/download/v%s/%s",
			daselFile, tm.daselVersion, daselFile),
		"popd >/dev/null 2>&1 || true + "}, "")
}

// DownloadCrio downloads the CRI-O tarball for the specified architecture and version.
// pushd "/tmp/provisioner/cri-o" >/dev/null 2>&1 || true
// curl -sSLo "cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz" "https://storage.googleapis.com/cri-o/artifacts/cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz"
// popd >/dev/null 2>&1 || true
func (tm *testHelper) DownloadCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", tm.architecture, tm.crioVersion)
	return automa_steps.NewBashScriptStep("download-crio", []string{
		fmt.Sprintf("pushd %s/crio >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo %s https://storage.googleapis.com/cri-o/artifacts/%s",
			crioFile, crioFile),
		"popd >/dev/null 2>&1 || true + "}, "")
}

func (tm *testHelper) DownloadKubernetesTools() automa.Builder {
	return automa.NewWorkFlowBuilder("download-kubernetes-tools").Steps(
		tm.DownloadKubeadm(),
		tm.DownloadKubelet(),
		tm.DownloadKubectl(),
		tm.DownloadK9s(),
		tm.DownloadHelm(),
		tm.DownloadDasel(),
		tm.DownloadKubernetesServiceFiles(),
	)
}

func (tm *testHelper) DownloadKubeadm() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubeadm", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo kubeadm https://dl.k8s.io/release/v%s/bin/%s/%s/kubeadm",
			tm.kubernetesVersion, tm.os, tm.architecture),
		"sudo chmod +x kubeadm",
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) DownloadKubelet() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubelet", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo kubelet https://dl.k8s.io/release/v%s/bin/%s/%s/kubelet",
			tm.kubernetesVersion, tm.os, tm.architecture),
		"sudo chmod +x kubelet",
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) DownloadKubectl() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubectl", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo kubectl https://dl.k8s.io/release/v%s/bin/%s/%s/kubectl",
			tm.kubernetesVersion, tm.os, tm.architecture),
		"sudo chmod +x kubectl",
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) DownloadK9s() automa.Builder {
	k9sFile := fmt.Sprintf("k9s_%s_%s.tar.gz", tm.os, tm.architecture)
	return automa_steps.NewBashScriptStep("download-k9s", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo %s https://github.com/derailed/k9s/releases/download/v%s/%s",
			k9sFile, tm.k9sVersion, k9sFile),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) DownloadHelm() automa.Builder {
	helmFile := fmt.Sprintf("helm-v%s-%s-%s.tar.gz", tm.helmVersion, tm.os, tm.architecture)
	return automa_steps.NewBashScriptStep("download-helm", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo %s https://get.helm.sh/%s",
			helmFile, helmFile),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) DownloadKubernetesServiceFiles() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubernetes-config-files", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("curl -sSLo kubelet.service https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubelet/kubelet.service",
			tm.krelVersion),
		fmt.Sprintf("curl -sSLo 10-kubeadm.conf https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubeadm/10-kubeadm.conf",
			tm.krelVersion),
		"popd >/dev/null 2>&1 || true ",
	}, "")
}

func (tm *testHelper) DownloadCilium() automa.Builder {
	ciliumFile := fmt.Sprintf("cilium-%s-%s.tar.gz", tm.os, tm.architecture)
	return automa.NewWorkFlowBuilder("download-cilium").Steps(
		automa_steps.NewBashScriptStep("download-cilium-cli", []string{
			fmt.Sprintf("pushd %s/cilium >/dev/null 2>&1 || true", tm.provisionerHomeDir),
			fmt.Sprintf("curl -sSLo %s \"https://github.com/cilium/cilium-cli/releases/download/v%s/%s",
				ciliumFile, tm.ciliumVersion, ciliumFile),
			fmt.Sprintf("curl -sSLo %s.sha256sum \"https://github.com/cilium/cilium-cli/releases/download/v%s/%s.sha256sum",
				ciliumFile, tm.ciliumVersion, ciliumFile),
			fmt.Sprintf("sha256sum -c %s.sha256sum", ciliumFile),
			"popd >/dev/null 2>&1 || true",
		}, ""))
}

// Setup Provisioner and Sandbox Folders
func (tm *testHelper) SetupProductionSandboxFolders() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-production-folders", []string{
		fmt.Sprintf("sudo mkdir -p %s", tm.provisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/bin", tm.provisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/logs", tm.provisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/bin", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/crio/keys", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/default", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/sysconfig", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/provisioner", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/containers/registries.conf.d", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/cni/net.d", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/nri/conf.d", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/kubernetes/pki", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/etcd", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/containers/storage", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/kubelet", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/crio", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/cilium", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/nri", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/containers/storage", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/crio/exits", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/logs/crio/pods", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/run/runc", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/libexec/crio", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/lib/systemd/system/kubelet.service.d", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/bin", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/man", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/oci-umount/oci-umount.d", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/bash-completion/completions", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/fish/completions", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/zsh/site-functions", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/opt/cni/bin", tm.sandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/opt/nri/plugins", tm.sandboxDir),
		fmt.Sprintf("sudo chown -R %d:%d %s", tm.uid, tm.gid, tm.provisionerHomeDir),
		fmt.Sprintf("sudo chown -R root:root %s", tm.sandboxDir),
	}, "")
}

// Setup Bind Mounts
func (tm *testHelper) SetupBindMounts() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-bind-mounts", []string{
		"sudo mkdir -p /etc/kubernetes /var/lib/kubelet /var/run/cilium",
		fmt.Sprintf("if ! grep -q '/etc/kubernetes' /etc/fstab; then echo '%s/etc/kubernetes /etc/kubernetes none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", tm.sandboxDir),
		fmt.Sprintf("if ! grep -q '/var/lib/kubelet' /etc/fstab; then echo '%s/var/lib/kubelet /var/lib/kubelet none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", tm.sandboxDir),
		fmt.Sprintf("if ! grep -q '/var/run/cilium' /etc/fstab; then echo '%s/var/run/cilium /var/run/cilium none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", tm.sandboxDir),
		"sudo systemctl daemon-reload",
		"sudo mount /etc/kubernetes || true",
		"sudo mount /var/lib/kubelet || true",
		"sudo mount /var/run/cilium || true",
	}, "")
}

func (tm *testHelper) InstallDasel() automa.Builder {
	daselFile := "dasel_" + tm.os + "_" + tm.architecture
	return automa_steps.NewBashScriptStep("install-dasel", []string{
		fmt.Sprintf("pushd %s/utils >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("sudo install -m 755 %s %s/dasel", daselFile, tm.sandboxBinDir),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) InstallCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", tm.architecture, tm.crioVersion)
	return automa_steps.NewBashScriptStep("install-crio", []string{
		fmt.Sprintf("pushd %s/crio >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("sudo tar -C %s/crio/unpack -zxvf %s", tm.provisionerHomeDir, crioFile),
		fmt.Sprintf("pushd %s/crio/unpack/cri-o >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("DESTDIR=%s SYSTEMDDIR=/usr/lib/systemd/system sudo -E bash ./install", tm.sandboxDir),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

func (tm *testHelper) InstallKubernetesTools() automa.Builder {
	return automa_steps.NewBashScriptStep("install-kubernetes-tools", []string{
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubeadm\" \"%s/kubeadm\"", tm.provisionerHomeDir, tm.sandboxBinDir),
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubelet\" \"%s/kubelet\"", tm.provisionerHomeDir, tm.sandboxBinDir),
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubectl\" \"%s/kubectl\"", tm.provisionerHomeDir, tm.sandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubeadm\" /usr/local/bin/kubeadm", tm.sandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubelet\" /usr/local/bin/kubelet", tm.sandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubectl\" /usr/local/bin/kubectl", tm.sandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/k9s\" /usr/local/bin/k9s", tm.sandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/helm\" /usr/local/bin/helm", tm.sandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/cilium\" /usr/local/bin/cilium", tm.sandboxBinDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/lib/systemd/system/kubelet.service.d", tm.sandboxDir),
		fmt.Sprintf("sudo cp \"%s/kubernetes/kubelet.service\" \"%s/usr/lib/systemd/system/kubelet.service\"", tm.provisionerHomeDir, tm.sandboxDir),
		fmt.Sprintf("sudo cp \"%s/kubernetes/10-kubeadm.conf\" \"%s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf\"", tm.provisionerHomeDir, tm.sandboxDir),
	}, "")
}

// Change kubelet service file to use the sandbox bin directory
func (tm *testHelper) ConfigureSandboxKubeletService() automa.Builder {
	return automa_steps.NewBashScriptStep("configure-kubelet-service", []string{
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service",
			tm.sandboxBinDir, tm.sandboxDir),
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf",
			tm.sandboxBinDir, tm.sandboxDir),
	}, "")
}

func (tm *testHelper) ConfigureSandboxCrio() automa.Builder {
	return automa.NewWorkFlowBuilder("configure-sandbox-crio").Steps(
		automa_steps.NewBashScriptStep("symlink-sandbox-crio", []string{
			fmt.Sprintf("sudo ln -sf %s/etc/containers /etc/containers", tm.sandboxDir),
		}, ""),
		tm.ConfigureSandboxCrioDefaults(),
		tm.ConfigureSandboxCrioService(),
		tm.UpdateCrioConfiguration(),
	)
}

// Change CRI-O service file to use the sandbox bin directory
func (tm *testHelper) ConfigureSandboxCrioService() automa.Builder {
	return automa_steps.NewBashScriptStep("configure-crio-service", []string{
		fmt.Sprintf("sudo sed -i 's|/usr/local/bin/crio|%s/crio|' %s/usr/lib/systemd/system/crio.service",
			tm.sandboxBinDir, tm.sandboxDir),
		fmt.Sprintf("sudo sed -i 's|/etc/sysconfig/crio|%s/etc/default/crio|' %s/usr/lib/systemd/system/crio.service",
			tm.sandboxDir, tm.sandboxDir),
	}, "")
}

func (tm *testHelper) ConfigureSandboxCrioDefaults() automa.Builder {
	return automa_steps.NewBashScriptStep("configure-crio-defaults", []string{
		fmt.Sprintf(`cat <<EOF | sudo tee %s/etc/default/crio >/dev/null
# /etc/default/crio

# use "--enable-metrics" and "--metrics-port value"
#CRIO_METRICS_OPTIONS="--enable-metrics"

#CRIO_NETWORK_OPTIONS=
#CRIO_STORAGE_OPTIONS=

# CRI-O configuration directory
CRIO_CONFIG_OPTIONS="--config-dir=%s/etc/crio/crio.conf.d"
EOF`, tm.sandboxDir, tm.sandboxDir),
	}, "")
}

func (tm *testHelper) UpdateCrioConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep("update-crio-configuration", []string{
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v runc '.crio.runtime.default_runtime'", tm.sandboxBinDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/etc/crio/keys\" '.crio.runtime.decryption_keys_path'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/exits\" '.crio.runtime.container_exits_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio\" '.crio.runtime.container_attach_socket_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run\" '.crio.runtime.namespaces_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/pinns\" '.crio.runtime.pinns_path'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxLocalBinDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/run/runc\" '.crio.runtime.runtimes.runc.runtime_root'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/crio.sock\" '.crio.api.listen'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/crio.sock\" '.crio.api.listen'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/lib/containers/storage\" '.crio.root'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/containers/storage\" '.crio.runroot'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/version\" '.crio.version_file'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/logs/crio/pods\" '.crio.log_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/lib/crio/clean.shutdown\" '.crio.clean_shutdown_file'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/etc/cni/net.d/\" '.crio.network.network_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/opt/cni/bin\" -s 'crio.network.plugin_dirs.[]'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/opt/nri/plugins\" '.crio.nri.nri_plugin_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/etc/nri/conf.d\" '.crio.nri.nri_plugin_config_dir'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/nri/nri.sock\" '.crio.nri.nri_listen'", tm.sandboxBinDir, tm.sandboxDir, tm.sandboxDir),
	}, "")
}

// Install K9s
func (tm *testHelper) InstallK9s() automa.Builder {
	return automa_steps.NewBashScriptStep("install-k9s", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("sudo tar -C %s -zxvf k9s_%s_%s.tar.gz k9s", tm.sandboxBinDir, tm.os, tm.architecture),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

// Install Helm
func (tm *testHelper) InstallHelm() automa.Builder {
	return automa_steps.NewBashScriptStep("install-helm", []string{
		fmt.Sprintf("pushd %s/kubernetes >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("sudo tar -C %s -zxvf helm-v%s-%s-%s.tar.gz %s-%s/helm --strip-components 1",
			tm.sandboxBinDir, tm.helmVersion, tm.os, tm.architecture, tm.os, tm.architecture),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

// Install Cilium
func (tm *testHelper) InstallCilium() automa.Builder {
	return automa_steps.NewBashScriptStep("install-cilium", []string{
		fmt.Sprintf("pushd %s/cilium >/dev/null 2>&1 || true", tm.provisionerHomeDir),
		fmt.Sprintf("sudo tar -C %s -zxvf cilium-%s-%s.tar.gz", tm.sandboxBinDir, tm.os, tm.architecture),
		"popd >/dev/null 2>&1 || true",
	}, "")
}

// Setup Systemd Service SymLinks
func (tm *testHelper) SetupSystemdServiceSymlinks() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-systemd-symlinks", []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service /usr/lib/systemd/system/kubelet.service", tm.sandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service.d /usr/lib/systemd/system/kubelet.service.d", tm.sandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service", tm.sandboxDir),
	}, "")
}

func (tm *testHelper) EnableAndStartServices() automa.Builder {
	return automa_steps.NewBashScriptStep("enable-start-services", []string{
		"sudo systemctl daemon-reload",
		"sudo systemctl enable crio kubelet",
		"sudo systemctl start crio kubelet",
	}, "")
}

// Torch prior KubeADM Configuration
func (tm *testHelper) TorchPriorKubeAdmConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep("torch-prior-kubeadm-config", []string{
		fmt.Sprintf("sudo %s/kubeadm reset --force || true", tm.sandboxBinDir),
		fmt.Sprintf("sudo rm -rf %s/etc/kubernetes/* %s/etc/cni/net.d/* %s/var/lib/etcd/* || true",
			tm.sandboxDir, tm.sandboxDir, tm.sandboxDir),
	}, "")
}

// Setup KubeADM Configuration
func (tm *testHelper) SetupKubeAdminConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-kubeadm-configuration", []string{
		"kube_bootstrap_token=\"$(" + tm.sandboxBinDir + "/kubeadm token generate)\"",
		"machine_ip=\"$(ip route get 1 | head -1 | sed 's/^.*src \\(.*\\)$/\\1/' | awk '{print $1}')\"",
		"cat <<EOF | sudo tee " + tm.sandboxDir + "/etc/provisioner/kubeadm-init.yaml >/dev/null\n" +
			"apiVersion: kubeadm.k8s.io/v1beta4\n" +
			"kind: InitConfiguration\n" +
			"bootstrapTokens:\n" +
			"  - groups:\n" +
			"    - system:bootstrappers:kubeadm:default-node-token\n" +
			"    token: ${kube_bootstrap_token}\n" +
			"    ttl: 720h0m0s\n" +
			"    usages:\n" +
			"      - signing\n" +
			"      - authentication\n" +
			"localAPIEndpoint:\n" +
			"  advertiseAddress: ${machine_ip}\n" +
			"  bindPort: 6443\n" +
			"nodeRegistration:\n" +
			"  criSocket: unix://" + tm.sandboxDir + "/var/run/crio/crio.sock\n" +
			"  imagePullPolicy: IfNotPresent\n" +
			"  imagePullSerial: true\n" +
			"  name: $(hostname)\n" +
			"  taints:\n" +
			"    - key: \"node.cilium.io/agent-not-ready\"\n" +
			"      value: \"true\"\n" +
			"      effect: \"NoExecute\"\n" +
			"  kubeletExtraArgs:\n" +
			"    - name: node-ip\n" +
			"      value: ${machine_ip}\n" +
			"skipPhases:\n" +
			"  - addon/kube-proxy\n" +
			"timeouts:\n" +
			"  controlPlaneComponentHealthCheck: 4m0s\n" +
			"  discovery: 5m0s\n" +
			"  etcdAPICall: 2m0s\n" +
			"  kubeletHealthCheck: 4m0s\n" +
			"  kubernetesAPICall: 1m0s\n" +
			"  tlsBootstrap: 5m0s\n" +
			"  upgradeManifests: 5m0s\n" +
			"---\n" +
			"apiVersion: kubeadm.k8s.io/v1beta4\n" +
			"kind: ClusterConfiguration\n" +
			"controlPlaneEndpoint: \"${machine_ip}:6443\"\n" +
			"certificatesDir: " + tm.sandboxDir + "/etc/kubernetes/pki\n" +
			"caCertificateValidityPeriod: 87600h0m0s\n" +
			"certificateValidityPeriod: 8760h0m0s\n" +
			"encryptionAlgorithm: RSA-2048\n" +
			"clusterName: k8s.main.gcp\n" +
			"etcd:\n" +
			"  local:\n" +
			"    dataDir: " + tm.sandboxDir + "/var/lib/etcd\n" +
			"imageRepository: registry.k8s.io\n" +
			"kubernetesVersion: " + tm.kubernetesVersion + "\n" +
			"networking:\n" +
			"  dnsDomain: cluster.local\n" +
			"  serviceSubnet: 10.0.0.0/14\n" +
			"  podSubnet: 10.4.0.0/14\n" +
			"controllerManager:\n" +
			"  extraArgs:\n" +
			"    - name: node-cidr-mask-size-ipv4\n" +
			"      value: \"24\"\n" +
			"EOF",
	}, "")
}

func (tm *testHelper) SetPipeFailMode() automa.Builder {
	return automa_steps.NewBashScriptStep("set-bash-strict-mode", []string{
		"set -eo pipefail",
	}, "")
}

func (tm *testHelper) ConfigureCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep("configure-cilium-cni", []string{
		"machine_ip=\"$(ip route get 1 | head -1 | sed 's/^.*src \\(.*\\)$/\\1/' | awk '{print $1}')\"",
		"cat <<EOF | sudo tee " + tm.sandboxDir + "/etc/provisioner/cilium-config.yaml >/dev/null\n" +
			"# StepSecurity Required Features\n" +
			"extraArgs:\n" +
			"  - --tofqdns-dns-reject-response-code=nameError\n" +
			"\n" +
			"# Hubble Support\n" +
			"hubble:\n" +
			"  relay:\n" +
			"    enabled: true\n" +
			"  ui:\n" +
			"    enabled: false\n" +
			"\n" +
			"# KubeProxy Replacement Config\n" +
			"kubeProxyReplacement: true\n" +
			"k8sServiceHost: ${machine_ip}\n" +
			"k8sServicePort: 6443\n" +
			"\n" +
			"# IP Version Support\n" +
			"ipam:\n" +
			"  mode: \"kubernetes\"\n" +
			"k8s:\n" +
			"  requireIPv4PodCIDR: true\n" +
			"  requireIPv6PodCIDR: false\n" +
			"ipv4:\n" +
			"  enabled: true\n" +
			"ipv6:\n" +
			"  enabled: false\n" +
			"\n" +
			"# Routing Configuration\n" +
			"routingMode: native\n" +
			"autoDirectNodeRoutes: true\n" +
			"#ipv4NativeRoutingCIDR: 10.128.0.0/20\n" +
			"\n" +
			"# Load Balancer Configuration\n" +
			"loadBalancer:\n" +
			"  mode: dsr\n" +
			"  dsrDispatch: opt\n" +
			"  algorithm: maglev\n" +
			"  acceleration: \"best-effort\"\n" +
			"  l7:\n" +
			"    backend: disabled\n" +
			"\n" +
			"nodePort:\n" +
			"  enabled: true\n" +
			"\n" +
			"hostPort:\n" +
			"  enabled: true\n" +
			"\n" +
			"# BPF & IP Masquerading Support\n" +
			"ipMasqAgent:\n" +
			"  enabled: true\n" +
			"  config:\n" +
			"    nonMasqueradeCIDRs: []\n" +
			"bpf:\n" +
			"  masquerade: true\n" +
			"  hostLegacyRouting: false\n" +
			"  lbExternalClusterIP: true\n" +
			"  preallocateMaps: true\n" +
			"\n" +
			"# Envoy DaemonSet Support\n" +
			"envoy:\n" +
			"  enabled: false\n" +
			"\n" +
			"# BGP Control Plane\n" +
			"bgpControlPlane:\n" +
			"  enabled: false\n" +
			"\n" +
			"# L2 Announcements\n" +
			"l2announcements:\n" +
			"  enabled: false\n" +
			"k8sClientRateLimit:\n" +
			"  qps: 100\n" +
			"  burst: 150\n" +
			"\n" +
			"# CNI Configuration\n" +
			"cni:\n" +
			"  binPath: " + tm.sandboxDir + "/opt/cni/bin\n" +
			"  confPath: " + tm.sandboxDir + "/etc/cni/net.d\n" +
			"\n" +
			"# DaemonSet Configuration\n" +
			"daemon:\n" +
			"  runPath: " + tm.sandboxDir + "/var/run/cilium\n" +
			"\n" +
			"EOF",
	}, "")
}

func (tm *testHelper) InstallCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep("install-cilium-cni", []string{
		fmt.Sprintf("sudo %s/cilium install --version \"%s\" --values %s/etc/provisioner/cilium-config.yaml",
			tm.sandboxBinDir, tm.ciliumVersion, tm.sandboxDir),
	}, "")
}

// Restart Container and Kubelet (fix for cilium CNI not initializing - CNI not ready error)
func (tm *testHelper) EnforceCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep("restart-crio-and-kubelet", []string{
		"sudo sysctl --system >/dev/null",
		"sudo systemctl restart kubelet crio",
		fmt.Sprintf("%s/cilium status --wait", tm.sandboxBinDir),
	}, "")
}

func (tm *testHelper) InstallMetalLB() automa.Builder {
	return automa_steps.NewBashScriptStep("install-metallb", []string{
		fmt.Sprintf("sudo %s/helm repo add metallb https://metallb.github.io/metallb", tm.sandboxBinDir),
		fmt.Sprintf("sudo %s/helm install metallb metallb/metallb --version %s \\\n"+
			"--set speaker.frr.enabled=false \\\n"+
			"--namespace metallb-system --create-namespace --atomic --wait",
			tm.sandboxBinDir, tm.metallbVersion),
		"sleep 60",
	}, "")
}

func (tm *testHelper) DeployMetallbConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep("deploy-metallb-config", []string{
		"machine_ip=\"$(ip route get 1 | head -1 | sed 's/^.*src \\(.*\\)$/\\1/' | awk '{print $1}')\"",
		"cat <<EOF | " + tm.sandboxBinDir + "/kubectl apply -f -\n" +
			"---\n" +
			"apiVersion: metallb.io/v1beta1\n" +
			"kind: IPAddressPool\n" +
			"metadata:\n" +
			"  name: private-address-pool\n" +
			"  namespace: metallb-system\n" +
			"spec:\n" +
			"  addresses:\n" +
			"    - 192.168.99.0/24\n" +
			"---\n" +
			"apiVersion: metallb.io/v1beta1\n" +
			"kind: IPAddressPool\n" +
			"metadata:\n" +
			"  name: public-address-pool\n" +
			"  namespace: metallb-system\n" +
			"spec:\n" +
			"  addresses:\n" +
			"    - ${machine_ip}/32\n" +
			"---\n" +
			"apiVersion: metallb.io/v1beta1\n" +
			"kind: L2Advertisement\n" +
			"metadata:\n" +
			"  name: primary-l2-advertisement\n" +
			"  namespace: metallb-system\n" +
			"spec:\n" +
			"  ipAddressPools:\n" +
			"  - private-address-pool\n" +
			"  - public-address-pool\n" +
			"EOF",
	}, "")
}

func TestSetupClusterUsingBashCommands(t *testing.T) {
	tm := newTestHelper()

	wf, err := automa.NewWorkFlowBuilder("setup-kubernetes-cluster").Steps(
		tm.UpdateOS(),
		tm.DisableSwap(),
		tm.RemoveContainerd(),
		tm.RemoveContainerd(),
		tm.InstallGnupg2(),
		tm.InstallConntrack(),
		tm.InstallSoCat(),
		tm.InstallEBTables(),
		tm.InstallNFTables(),
		tm.EnableAndStartNFTables(),
		tm.AutoRemovePackages(),
		tm.InstallKernelModules(),
		tm.ConfigureKernelModules(),
		tm.setupWorkingDirectories(),
		tm.DownloadDasel(),
		tm.DownloadCrio(),
	).Build()
	require.NoError(t, err)

	report, err := wf.Execute(context.Background())
	require.NoError(t, err)
	require.Equal(t, automa.StatusSuccess, report.Status)
	for i, step := range report.StepReports {
		fmt.Printf("%d. %s: %s\n", i, step.Id, step.Status)
		require.Equal(t, automa.StatusSuccess, step.Status, "step %q failed: %s", step.Id, step.Error)
	}
}
