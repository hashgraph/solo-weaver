package steps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

var th = newTestHelper()

// testHelper mimics the bash script commands to help us create a test scenario for the
// actual step implementation and testing. This only assumes a debian or ubuntu OS
type testHelper struct {
	userHomeDir        string
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
	logger             *zerolog.Logger
}

func newTestHelper() *testHelper {
	return &testHelper{
		userHomeDir:        os.Getenv("HOME"),
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
		daselVersion:       "2.8.1",
		logger:             logx.As(),
	}
}

func (tm *testHelper) UpdateOS() automa.Builder {
	return automa_steps.NewBashScriptStep("update-os", []string{
		"sudo apt-get update -y",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DisableSwap() automa.Builder {
	return automa_steps.NewBashScriptStep("disable-swap", []string{
		`sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab`,
		`sudo swapoff -a`,
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) RemoveContainerd() automa.Builder {
	return automa_steps.NewBashScriptStep("remove-containerd", []string{
		"sudo apt-get remove -y containerd containerd.io || true",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallIPTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install-iptables", []string{
		"sudo apt-get install -y iptables",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallGnupg2() automa.Builder {
	return automa_steps.NewBashScriptStep("install-gnupg2", []string{
		"sudo apt-get install -y gnupg2",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallConntrack() automa.Builder {
	return automa_steps.NewBashScriptStep("install-conntrack", []string{
		"sudo apt-get install -y conntrack",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallSoCat() automa.Builder {
	return automa_steps.NewBashScriptStep("install-socat", []string{
		"sudo apt-get install -y socat",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallEBTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install-ebtables", []string{
		"sudo apt-get install -y ebtables",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install-nftables", []string{
		"sudo apt-get install -y nftables",
	}, "", automa.WithLogger(*tm.logger))
}

// Enable nftables service
func (tm *testHelper) EnableAndStartNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep("enable-start-nftables", []string{
		"sudo systemctl enable nftables",
		"sudo systemctl start nftables",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) AutoRemovePackages() automa.Builder {
	return automa_steps.NewBashScriptStep("auto-remove-packages", []string{
		"sudo apt-get autoremove -y",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallKernelModules() automa.Builder {
	return automa_steps.NewBashScriptStep("install-kernel-modules", []string{
		"sudo modprobe overlay",
		"sudo modprobe br_netfilter",
		`echo 'overlay' | sudo tee /etc/modules-load.d/overlay.conf`,
		`echo 'br_netfilter' | sudo tee /etc/modules-load.d/br_netfilter.conf`,
	}, "", automa.WithLogger(*tm.logger))
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
	}, "", automa.WithLogger(*tm.logger))
}

// Download dasel
func (tm *testHelper) DownloadDasel() automa.Builder {
	daselFile := "dasel_" + tm.os + "_" + tm.architecture
	fmt.Println(fmt.Sprintf("curl -sSLo %s https://github.com/TomWright/dasel/releases/download/v%s/%s", daselFile, tm.daselVersion, daselFile))
	return automa_steps.NewBashScriptStep("download-dasel", []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/TomWright/dasel/releases/download/v%s/%s",
			daselFile, tm.daselVersion, daselFile),
	}, fmt.Sprintf("%s/utils", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

// DownloadCrio downloads the CRI-O tarball for the specified architecture and version.
// pushd "/tmp/provisioner/cri-o" >/dev/null 2>&1 || true
// curl -sSLo "cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz" "https://storage.googleapis.com/cri-o/artifacts/cri-o.${ARCH}.v${CRIO_VERSION}.tar.gz"
// popd >/dev/null 2>&1 || true
func (tm *testHelper) DownloadCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", tm.architecture, tm.crioVersion)
	return automa_steps.NewBashScriptStep("download-crio", []string{
		fmt.Sprintf("curl -sSLo %s https://storage.googleapis.com/cri-o/artifacts/%s",
			crioFile, crioFile),
	}, fmt.Sprintf("%s/crio", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
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
		fmt.Sprintf("curl -sSLo kubeadm https://dl.k8s.io/release/v%s/bin/%s/%s/kubeadm",
			tm.kubernetesVersion, tm.os, tm.architecture),
		"sudo chmod +x kubeadm",
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DownloadKubelet() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubelet", []string{
		fmt.Sprintf("curl -sSLo kubelet https://dl.k8s.io/release/v%s/bin/%s/%s/kubelet",
			tm.kubernetesVersion, tm.os, tm.architecture),
		"sudo chmod +x kubelet",
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DownloadKubectl() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubectl", []string{
		fmt.Sprintf("curl -sSLo kubectl https://dl.k8s.io/release/v%s/bin/%s/%s/kubectl",
			tm.kubernetesVersion, tm.os, tm.architecture),
		"sudo chmod +x kubectl",
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DownloadK9s() automa.Builder {
	k9sFile := fmt.Sprintf("k9s_%s_%s.tar.gz", tm.os, tm.architecture)
	return automa_steps.NewBashScriptStep("download-k9s", []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/derailed/k9s/releases/download/v%s/%s",
			k9sFile, tm.k9sVersion, k9sFile),
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DownloadHelm() automa.Builder {
	helmFile := fmt.Sprintf("helm-v%s-%s-%s.tar.gz", tm.helmVersion, tm.os, tm.architecture)
	return automa_steps.NewBashScriptStep("download-helm", []string{
		fmt.Sprintf("curl -sSLo %s https://get.helm.sh/%s",
			helmFile, helmFile),
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DownloadKubernetesServiceFiles() automa.Builder {
	return automa_steps.NewBashScriptStep("download-kubernetes-config-files", []string{
		fmt.Sprintf("curl -sSLo kubelet.service https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubelet/kubelet.service",
			tm.krelVersion),
		fmt.Sprintf("curl -sSLo 10-kubeadm.conf https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubeadm/10-kubeadm.conf",
			tm.krelVersion),
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DownloadCilium() automa.Builder {
	ciliumFile := fmt.Sprintf("cilium-%s-%s.tar.gz", tm.os, tm.architecture)
	fmt.Println(fmt.Sprintf("curl -sSLo %s https://github.com/cilium/cilium-cli/releases/download/v%s/%s",
		ciliumFile, tm.ciliumCliVersion, ciliumFile))
	return automa_steps.NewBashScriptStep("download-cilium-cli", []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/cilium/cilium-cli/releases/download/v%s/%s",
			ciliumFile, tm.ciliumCliVersion, ciliumFile),
		fmt.Sprintf("curl -sSLo %s.sha256sum https://github.com/cilium/cilium-cli/releases/download/v%s/%s.sha256sum",
			ciliumFile, tm.ciliumCliVersion, ciliumFile),
		fmt.Sprintf("sha256sum -c %s.sha256sum", ciliumFile),
	}, fmt.Sprintf("%s/cilium", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
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
	}, "", automa.WithLogger(*tm.logger))
}

// Setup Bind Mounts
func (tm *testHelper) SetupBindMounts() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-bind-mounts", []string{
		"sudo mkdir -p /etc/kubernetes /var/lib/kubelet /var/run/cilium",
		fmt.Sprintf("if ! grep -q '/etc/kubernetes' /etc/fstab; then echo '%s/etc/kubernetes /etc/kubernetes none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", tm.sandboxDir),
		fmt.Sprintf("if ! grep -q '/var/lib/kubelet' /etc/fstab; then echo '%s/var/lib/kubelet /var/lib/kubelet none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", tm.sandboxDir),
		fmt.Sprintf("if ! grep -q '/var/run/cilium' /etc/fstab; then echo '%s/var/run/cilium /var/run/cilium none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", tm.sandboxDir),
		"sudo systemctl daemon-reload",
		"sudo mount /etc/kubernetes",
		"sudo mount /var/lib/kubelet",
		"sudo mount /var/run/cilium",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallDasel() automa.Builder {
	daselFile := "dasel_" + tm.os + "_" + tm.architecture
	return automa_steps.NewBashScriptStep("install-dasel", []string{
		fmt.Sprintf("sudo install -m 755 %s %s/dasel", daselFile, tm.sandboxBinDir),
	}, fmt.Sprintf("%s/utils", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", tm.architecture, tm.crioVersion)
	return automa_steps.NewBashScriptStep("install-crio", []string{
		fmt.Sprintf("sudo tar -C %s/crio/unpack -zxvf %s", tm.provisionerHomeDir, crioFile),
		fmt.Sprintf(`pushd %s/crio/unpack/cri-o >/dev/null 2>&1 || true;,
		DESTDIR=%s SYSTEMDDIR=/usr/lib/systemd/system sudo -E bash ./install; 
		popd >/dev/null 2>&1 || true`, tm.provisionerHomeDir, tm.sandboxDir),
	}, fmt.Sprintf("%s/crio", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
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
	}, "", automa.WithLogger(*tm.logger))
}

// Change kubelet service file to use the sandbox bin directory
func (tm *testHelper) ConfigureSandboxKubeletService() automa.Builder {
	return automa_steps.NewBashScriptStep("configure-kubelet-service", []string{
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service",
			tm.sandboxBinDir, tm.sandboxDir),
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf",
			tm.sandboxBinDir, tm.sandboxDir),
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) ConfigureSandboxCrio() automa.Builder {
	return automa.NewWorkFlowBuilder("configure-sandbox-crio").Steps(
		automa_steps.NewBashScriptStep("symlink-sandbox-crio", []string{
			fmt.Sprintf("sudo ln -sf %s/etc/containers /etc/containers", tm.sandboxDir),
		}, ""),
		tm.ConfigureSandboxCrioDefaults(),
		tm.ConfigureSandboxCrioService(),
		tm.UpdateCrioConfiguration(),
		tm.SetupCrioServiceSymlinks(),
	)
}

// Change CRI-O service file to use the sandbox bin directory
func (tm *testHelper) ConfigureSandboxCrioService() automa.Builder {
	return automa_steps.NewBashScriptStep("configure-crio-service", []string{
		fmt.Sprintf("sudo sed -i 's|/usr/local/bin/crio|%s/crio|' %s/usr/lib/systemd/system/crio.service",
			tm.sandboxLocalBinDir, tm.sandboxDir),
		fmt.Sprintf("sudo sed -i 's|-/etc/sysconfig/crio|%s/etc/default/crio|' %s/usr/lib/systemd/system/crio.service",
			tm.sandboxDir, tm.sandboxDir),
	}, "", automa.WithLogger(*tm.logger))
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
	}, "", automa.WithLogger(*tm.logger))
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
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) SetupCrioServiceSymlinks() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-crio-symlinks", []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service", tm.sandboxDir),
	}, "", automa.WithLogger(*tm.logger))
}

// Install K9s
func (tm *testHelper) InstallK9s() automa.Builder {
	return automa_steps.NewBashScriptStep("install-k9s", []string{
		fmt.Sprintf("sudo tar -C %s -zxvf k9s_%s_%s.tar.gz k9s", tm.sandboxBinDir, tm.os, tm.architecture),
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

// Install Helm
func (tm *testHelper) InstallHelm() automa.Builder {
	return automa_steps.NewBashScriptStep("install-helm", []string{
		fmt.Sprintf("sudo tar -C %s -zxvf helm-v%s-%s-%s.tar.gz %s-%s/helm --strip-components 1",
			tm.sandboxBinDir, tm.helmVersion, tm.os, tm.architecture, tm.os, tm.architecture),
	}, fmt.Sprintf("%s/kubernetes", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

// Install Cilium
func (tm *testHelper) InstallCilium() automa.Builder {
	return automa_steps.NewBashScriptStep("install-cilium", []string{
		fmt.Sprintf("sudo tar -C %s -zxvf cilium-%s-%s.tar.gz", tm.sandboxBinDir, tm.os, tm.architecture),
	}, fmt.Sprintf("%s/cilium", tm.provisionerHomeDir), automa.WithLogger(*tm.logger))
}

// Setup Systemd Service SymLinks
func (tm *testHelper) SetupSystemdServiceSymlinks() automa.Builder {
	return automa_steps.NewBashScriptStep("setup-systemd-symlinks", []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service /usr/lib/systemd/system/kubelet.service", tm.sandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service.d /usr/lib/systemd/system/kubelet.service.d", tm.sandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service", tm.sandboxDir),
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) EnableAndStartServices() automa.Builder {
	return automa_steps.NewBashScriptStep("enable-start-services", []string{
		"sudo systemctl daemon-reload",
		"sudo systemctl enable crio kubelet",
		"sudo systemctl start crio kubelet",
	}, "", automa.WithLogger(*tm.logger))
}

// Torch prior KubeADM Configuration
func (tm *testHelper) TorchPriorKubeAdmConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep("torch-prior-kubeadm-config", []string{
		fmt.Sprintf("sudo %s/kubeadm reset --force || true", tm.sandboxBinDir),
		fmt.Sprintf("sudo rm -rf %s/etc/kubernetes/* %s/etc/cni/net.d/* %s/var/lib/etcd/* || true",
			tm.sandboxDir, tm.sandboxDir, tm.sandboxDir),
	}, "", automa.WithLogger(*tm.logger))
}

func bashCommandOutput(script string) string {
	out, err := exec.Command("bash", "-c", script).Output()
	if err != nil {
		fmt.Println("Error:", err)
		return ""
	}
	val := strings.TrimSpace(string(out))
	return val
}

func generateKubeadmToken() string {
	// 3 bytes = 6 hex chars, 8 bytes = 16 hex chars
	b := make([]byte, 11)
	_, err := rand.Read(b)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s.%s", hex.EncodeToString(b[:3]), hex.EncodeToString(b[3:]))
}

// Setup KubeADM Configuration
func (tm *testHelper) SetupKubeAdminConfiguration() automa.Builder {
	hostname := bashCommandOutput("hostname")
	kubeBootstrapToken := "k7enhy.umvij8dtg59ksnqj" //  // bashCommandOutput(fmt.Sprintf("%s/kubeadm token generate", th.sandboxBinDir))
	machineIp := bashCommandOutput(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)

	fmt.Printf("hostname: %s\n", hostname)
	fmt.Printf("kube_bootstrap_token: %s\n", kubeBootstrapToken)
	fmt.Printf("machine_ip: %s\n", machineIp)
	configScript :=
		fmt.Sprintf(`cat <<EOF | sudo tee %s/etc/provisioner/kubeadm-init.yaml >/dev/null\n
apiVersion: kubeadm.k8s.io/v1beta4
kind: InitConfiguration
bootstrapTokens:
  - groups:
    - system:bootstrappers:kubeadm:default-node-token
    token: %s
    ttl: 720h0m0s
    usages:
      - signing
      - authentication
localAPIEndpoint:
  advertiseAddress: %s
  bindPort: 6443
nodeRegistration:
  criSocket: unix://%s/var/run/crio/crio.sock
  imagePullPolicy: IfNotPresent
  imagePullSerial: true
  name: %s
  taints:
    - key: "node.cilium.io/agent-not-ready" 
      value: "true"
      effect: "NoExecute"
  kubeletExtraArgs: 
    - name: node-ip
      value: %s
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
controlPlaneEndpoint: "%s:6443"
certificatesDir: %s/etc/kubernetes/pki
caCertificateValidityPeriod: 87600h0m0s
certificateValidityPeriod: 8760h0m0s
encryptionAlgorithm: RSA-2048
clusterName: k8s.main.gcp
etcd:
  local:
    dataDir: %s/var/lib/etcd
imageRepository: registry.k8s.io
kubernetesVersion: "%s"
networking:
  dnsDomain: cluster.local
  serviceSubnet: 10.0.0.0/14
  podSubnet: 10.4.0.0/14
controllerManager:
  extraArgs:
    - name: node-cidr-mask-size-ipv4
      value: "24"
EOF`, tm.sandboxDir, kubeBootstrapToken, machineIp, tm.sandboxDir, hostname, machineIp, machineIp,
			tm.sandboxDir, tm.sandboxDir, tm.kubernetesVersion)

	return automa_steps.NewBashScriptStep("setup-kubeadm-configuration", []string{
		configScript,
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) SetPipeFailMode() automa.Builder {
	return automa_steps.NewBashScriptStep("set-bash-strict-mode", []string{
		"set -eo pipefail",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InitializeKubernetesCluster() automa.Builder {
	return automa_steps.NewBashScriptStep("initialize-kubernetes-cluster", []string{
		fmt.Sprintf("sudo %s/kubeadm init --upload-certs --config %s/etc/provisioner/kubeadm-init.yaml",
			tm.sandboxBinDir, tm.sandboxDir),
		fmt.Sprintf("mkdir -p \"%s/.kube\"", tm.userHomeDir),
		fmt.Sprintf("sudo cp -f %s/etc/kubernetes/admin.conf \"%s/.kube/config\"",
			tm.sandboxDir, tm.userHomeDir),
		fmt.Sprintf("sudo chown \"%d:%d\" \"%s/.kube/config\"", tm.uid, tm.gid, tm.userHomeDir),
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) ConfigureCiliumCNI() automa.Builder {
	machineIp := bashCommandOutput(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)
	configScript := fmt.Sprintf(
		`cat <<EOF | sudo tee %s/etc/provisioner/cilium-config.yaml >/dev/null
# StepSecurity Required Features
extraArgs:
  - --tofqdns-dns-reject-response-code=nameError

# Hubble Support
hubble:
  relay:
    enabled: true
  ui:
    enabled: false

# KubeProxy Replacement Config
kubeProxyReplacement: true
k8sServiceHost: %s
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
  binPath:  "%s/opt/cni/bin"
  confPath: "%s/etc/cni/net.d"

# DaemonSet Configuration
daemon:
  runPath: "%s/var/run/cilium"
EOF`, tm.sandboxDir, machineIp, tm.sandboxDir, tm.sandboxDir, tm.sandboxDir)
	fmt.Printf("machine_ip: %s\n", machineIp)
	fmt.Println(configScript)
	return automa_steps.NewBashScriptStep("configure-cilium-cni", []string{
		configScript,
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep("install-cilium-cni", []string{
		fmt.Sprintf("sudo %s/cilium install --version \"%s\" --values %s/etc/provisioner/cilium-config.yaml",
			tm.sandboxBinDir, tm.ciliumVersion, tm.sandboxDir),
	}, "", automa.WithLogger(*tm.logger))
}

// Restart Container and Kubelet (fix for cilium CNI not initializing - CNI not ready error)
func (tm *testHelper) EnforceCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep("restart-crio-and-kubelet", []string{
		"sudo sysctl --system >/dev/null",
		"sudo systemctl restart kubelet crio",
		fmt.Sprintf("%s/cilium status --wait", tm.sandboxBinDir),
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) InstallMetalLB() automa.Builder {
	return automa_steps.NewBashScriptStep("install-metallb", []string{
		fmt.Sprintf("sudo %s/helm repo add metallb https://metallb.github.io/metallb", tm.sandboxBinDir),
		fmt.Sprintf("sudo %s/helm install metallb metallb/metallb --version %s \\\n"+
			"--set speaker.frr.enabled=false \\\n"+
			"--namespace metallb-system --create-namespace --atomic --wait",
			tm.sandboxBinDir, tm.metallbVersion),
		"sleep 60",
	}, "", automa.WithLogger(*tm.logger))
}

func (tm *testHelper) DeployMetallbConfiguration() automa.Builder {
	machineIp := bashCommandOutput(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)
	configScript := fmt.Sprintf(
		`cat <<EOF | %s/kubectl apply -f - 
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
    - %s/32
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
EOF`, tm.sandboxBinDir, machineIp)
	fmt.Println(configScript)
	return automa_steps.NewBashScriptStep("deploy-metallb-config", []string{
		configScript,
	}, "", automa.WithLogger(*tm.logger))
}

func BashScriptBasedClusterSetupWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("setup-kubernetes-cluster").Steps(
		th.UpdateOS(),
		th.DisableSwap(),
		th.RemoveContainerd(),
		th.RemoveContainerd(),
		th.InstallGnupg2(),
		th.InstallConntrack(),
		th.InstallSoCat(),
		th.InstallEBTables(),
		th.InstallNFTables(),
		th.EnableAndStartNFTables(),
		th.AutoRemovePackages(),
		th.InstallKernelModules(),
		th.ConfigureKernelModules(),
		th.setupWorkingDirectories(),
		th.DownloadDasel(),
		th.DownloadCrio(),
		th.DownloadKubernetesTools(),
		th.DownloadCilium(),
		th.SetupProductionSandboxFolders(),
		th.SetupBindMounts(),
		th.InstallDasel(),
		th.InstallCrio(),
		th.InstallKubernetesTools(),
		th.ConfigureSandboxKubeletService(),
		th.ConfigureSandboxCrio(),
		th.SetupSystemdServiceSymlinks(),
		th.InstallK9s(),
		th.InstallHelm(),
		th.InstallCilium(),
		th.SetupSystemdServiceSymlinks(),
		th.EnableAndStartServices(),
		th.TorchPriorKubeAdmConfiguration(),
		th.SetupKubeAdminConfiguration(),
		th.SetPipeFailMode(),
		th.InitializeKubernetesCluster(),
		th.ConfigureCiliumCNI(),
		th.InstallCiliumCNI(),
		th.EnforceCiliumCNI(),
		th.InstallMetalLB(),
		th.DeployMetallbConfiguration(),
	)
}

func TestSetupClusterUsingBashCommands(t *testing.T) {
	wf, err := BashScriptBasedClusterSetupWorkflow().Build()
	require.NoError(t, err)

	report, err := wf.Execute(context.Background())
	require.NoError(t, err)
	b, _ := yaml.Marshal(report)
	fmt.Printf("Workflow Execution Report:%s\n", b)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
