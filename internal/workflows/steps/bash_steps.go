package steps

import (
	"fmt"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"golang.hedera.com/solo-provisioner/internal/core"
	"os"
	"runtime"
)

// TODO: Move these constants to the appropriate step files once the implementation is done
const (
	UpdateOSStepId         = "update-OS"
	RemoveContainerdStepId = "remove-containerd"

	InstallIPTablesStepId  = "install-iptables"
	InstallGnupg2StepId    = "install-gnupg2"
	InstallConntrackStepId = "install-conntrack"
	InstallSoCatStepId     = "install-socat"
	InstallEBTablesStepId  = "install-ebtables"
	InstallNFTablesStepId  = "install-nftables"

	EnableStartNFTablesStepId    = "enable-start-nftables"
	AutoRemovePackagesStepId     = "auto-remove-packages"
	InstallKernelModulesStepId   = "install-kernel-modules"
	ConfigureKernelModulesStepId = "configure-kernel-modules"
	SetupWorkingDirsStepId       = "setup-working-dirs"

	DownloadDaselStepId                  = "download-dasel"
	DownloadCrioStepId                   = "download-crio"
	DownloadKubernetesToolsStepId        = "download-kubernetes-tools"
	DownloadKubeAdmStepId                = "download-kubeadm"
	DownloadKubectlStepId                = "download-kubectl"
	DownloadHelmStepId                   = "download-helm"
	DownloadK9sStepId                    = "download-k9s"
	DownloadKubeletStepId                = "download-kubelet"
	DownloadKubernetesServiceFilesStepId = "download-kubernetes-service-files"
	DownloadCiliumStepId                 = "download-cilium-cli"

	SetupSandboxFoldersStepId = "setup-sandbox-folders"
	SetupBindMountsStepId     = "setup-bind-mounts"

	InstallDaselStepId           = "install-dasel"
	InstallCrioStepId            = "install-crio"
	InstallKubernetesToolsStepId = "install-kubernetes-tools"
	InstallK9sStepId             = "install-k9s"
	InstallHelmStepId            = "install-helm"
	InstallCiliumStepId          = "install-cilium"
	InstallCiliumCNIStepId       = "install-cilium-cni"
	InstallMetalLBStepId         = "install-metallb"

	ConfigureSandboxKubeletSvcStepId   = "configure-kubelet-service"
	ConfigureSandboxCrioStepId         = "configure-sandbox-crio"
	ConfigureSandboxCrioSvcStepId      = "configure-sandbox-crio-service"
	ConfigureSandboxCrioDefaultsStepId = "configure-sandbox-crio-default"
	UpdateCrioConfStepId               = "update-crio-configuration"
	SetupCrioSymlinkStepId             = "setup-crio-symlinks"
	SetupSystemdServiceSymlinksStepId  = "setup-systemd-symlinks"
	EnableStartServicesStepId          = "enable-start-services"
	TorchPriorKubeAdmConfigStepId      = "torch-prior-kubeadm-config"
	SetupKubeAdminConfigStepId         = "setup-kubeadm-configuration"
	SetBashPipeFailModeStepId          = "set-bash-pipefail-mode"
	InitializeKubernetesClusterStepId  = "initialize-kubernetes-cluster"
	ConfigureCiliumCNIStepId           = "configure-cilium-cni"
	EnforceCiliumCNIStepId             = "restart-crio-and-kubelet"
	DeployMetallbConfigStepId          = "deploy-metallb-config"
	CheckClusterHealthStepId           = "check-cluster-health"
	SetupClusterStepId                 = "setup-kubernetes-cluster"
)

// initialized in init()
var (
	bashSteps         bashScriptStep
	bashStepsRegistry automa.Registry
)

// bashScriptStep mimics and encapsulates the bash script commands to help us create a test scenario for the
// actual step implementation and testing. This only assumes a debian or ubuntu OS.
//
// This is not intended for production use, but to help us create a test scenario that we would do by
// manually running the bash commands.
//
// Note: This struct shouldn't be instantiated repeatedly as it is just meant to encapsulate the core steps without
// interfering with the actual implementation of the steps. So users should just use bashSteps variable or
// the bashStepsRegistry to access the step implementation, which are initialized in the init(). These variables
// also enable us to mock as required in our unit tests.
type bashScriptStep struct {
	HomeDir            string
	OS                 string
	ARCH               string
	UID                int
	GID                int
	ProvisionerHomeDir string
	SandboxDir         string
	SandboxBinDir      string
	SandboxLocalBinDir string
	CrioVersion        string
	KubernetesVersion  string
	KrelVersion        string
	K9sVersion         string
	HelmVersion        string
	CiliumCliVersion   string
	CiliumVersion      string
	MetallbVersion     string
	DaselVersion       string
	MachineIp          string
	Hostname           string
	KubeBootstrapToken string
}

func initBashScriptSteps() bashScriptStep {
	machineIp, _ := RunCmdOutput(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)
	hostname, _ := RunCmdOutput("hostname")
	kubeBootstrapToken := generateKubeadmToken() // e.g: "k7enhy.umvij8dtg59ksnqj"

	return bashScriptStep{
		HomeDir:            os.Getenv("HOME"),
		OS:                 runtime.GOOS,
		ARCH:               runtime.GOARCH,
		UID:                os.Getuid(),
		GID:                os.Getgid(),
		ProvisionerHomeDir: core.ProvisionerHomeDir,
		SandboxDir:         core.SandboxDir,
		SandboxBinDir:      core.SandboxBinDir,
		SandboxLocalBinDir: core.SandboxLocalBinDir,
		CrioVersion:        "1.33.4",
		KubernetesVersion:  "1.33.4",
		KrelVersion:        "v0.18.0",
		K9sVersion:         "0.50.9",
		HelmVersion:        "3.18.6",
		CiliumCliVersion:   "0.18.7",
		CiliumVersion:      "1.18.1",
		MetallbVersion:     "0.15.2",
		DaselVersion:       "2.8.1",
		MachineIp:          machineIp,
		Hostname:           hostname,
		KubeBootstrapToken: kubeBootstrapToken,
	}
}

func (b *bashScriptStep) UpdateOS() automa.Builder {
	return automa_steps.NewBashScriptStep(UpdateOSStepId, []string{
		"sudo apt-get update -y",
	}, "")
}

func (b *bashScriptStep) DisableSwap() automa.Builder {
	return automa_steps.NewBashScriptStep(DisableSwapStepId, []string{
		`sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab`,
		`sudo swapoff -a`,
	}, "")
}

func (b *bashScriptStep) RemoveContainerd() automa.Builder {
	return automa_steps.NewBashScriptStep(RemoveContainerdStepId, []string{
		"sudo apt-get remove -y containerd containerd.io || true",
	}, "")
}

func (b *bashScriptStep) InstallIPTables() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallIPTablesStepId, []string{
		"sudo apt-get install -y iptables",
	}, "")
}

func (b *bashScriptStep) InstallGnupg2() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallGnupg2StepId, []string{
		"sudo apt-get install -y gnupg2",
	}, "")
}

func (b *bashScriptStep) InstallConntrack() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallConntrackStepId, []string{
		"sudo apt-get install -y conntrack",
	}, "")
}

func (b *bashScriptStep) InstallSoCat() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallSoCatStepId, []string{
		"sudo apt-get install -y socat",
	}, "")
}

func (b *bashScriptStep) InstallEBTables() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallEBTablesStepId, []string{
		"sudo apt-get install -y ebtables",
	}, "")
}

func (b *bashScriptStep) InstallNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallNFTablesStepId, []string{
		"sudo apt-get install -y nftables",
	}, "")
}

func (b *bashScriptStep) EnableAndStartNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep(EnableStartNFTablesStepId, []string{
		"sudo systemctl enable nftables",
		"sudo systemctl start nftables",
	}, "")
}

func (b *bashScriptStep) AutoRemovePackages() automa.Builder {
	return automa_steps.NewBashScriptStep(AutoRemovePackagesStepId, []string{
		"sudo apt-get autoremove -y",
	}, "")
}

func (b *bashScriptStep) InstallKernelModules() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallKernelModulesStepId, []string{
		"sudo modprobe overlay",
		"sudo modprobe br_netfilter",
		`echo 'overlay' | sudo tee /etc/modules-load.d/overlay.conf`,
		`echo 'br_netfilter' | sudo tee /etc/modules-load.d/br_netfilter.conf`,
	}, "")
}

func (b *bashScriptStep) ConfigureKernelModules() automa.Builder {
	return automa.NewWorkFlowBuilder(ConfigureKernelModulesStepId).Steps(
		automa_steps.NewBashScriptStep("cleanup-sysctl-configs", []string{
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

func (b *bashScriptStep) DownloadDasel() automa.Builder {
	daselFile := "dasel_" + b.OS + "_" + b.ARCH
	return automa_steps.NewBashScriptStep(DownloadDaselStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/TomWright/dasel/releases/download/v%s/%s",
			daselFile, b.DaselVersion, daselFile),
	}, fmt.Sprintf("%s/utils", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", b.ARCH, b.CrioVersion)
	return automa_steps.NewBashScriptStep(DownloadCrioStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://storage.googleapis.com/cri-o/artifacts/%s",
			crioFile, crioFile),
	}, fmt.Sprintf("%s/crio", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubernetesTools() automa.Builder {
	return automa.NewWorkFlowBuilder(DownloadKubernetesToolsStepId).Steps(
		b.DownloadKubeadm(),
		b.DownloadKubelet(),
		b.DownloadKubectl(),
		b.DownloadK9s(),
		b.DownloadHelm(),
		b.DownloadDasel(),
		b.DownloadKubernetesServiceFiles(),
	)
}

func (b *bashScriptStep) DownloadKubeadm() automa.Builder {
	return automa_steps.NewBashScriptStep(DownloadKubeAdmStepId, []string{
		fmt.Sprintf("curl -sSLo kubeadm https://dl.k8s.io/release/v%s/bin/%s/%s/kubeadm",
			b.KubernetesVersion, b.OS, b.ARCH),
		"sudo chmod +x kubeadm",
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubelet() automa.Builder {
	return automa_steps.NewBashScriptStep(DownloadKubeletStepId, []string{
		fmt.Sprintf("curl -sSLo kubelet https://dl.k8s.io/release/v%s/bin/%s/%s/kubelet",
			b.KubernetesVersion, b.OS, b.ARCH),
		"sudo chmod +x kubelet",
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubectl() automa.Builder {
	return automa_steps.NewBashScriptStep(DownloadKubectlStepId, []string{
		fmt.Sprintf("curl -sSLo kubectl https://dl.k8s.io/release/v%s/bin/%s/%s/kubectl",
			b.KubernetesVersion, b.OS, b.ARCH),
		"sudo chmod +x kubectl",
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadK9s() automa.Builder {
	k9sFile := fmt.Sprintf("k9s_%s_%s.tar.gz", b.OS, b.ARCH)
	return automa_steps.NewBashScriptStep(DownloadK9sStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/derailed/k9s/releases/download/v%s/%s",
			k9sFile, b.K9sVersion, k9sFile),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadHelm() automa.Builder {
	helmFile := fmt.Sprintf("helm-v%s-%s-%s.tar.gz", b.HelmVersion, b.OS, b.ARCH)
	return automa_steps.NewBashScriptStep(DownloadHelmStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://get.helm.sh/%s",
			helmFile, helmFile),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubernetesServiceFiles() automa.Builder {
	return automa_steps.NewBashScriptStep(DownloadKubernetesServiceFilesStepId, []string{
		fmt.Sprintf("curl -sSLo kubelet.service https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubelet/kubelet.service",
			b.KrelVersion),
		fmt.Sprintf("curl -sSLo 10-kubeadm.conf https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubeadm/10-kubeadm.conf",
			b.KrelVersion),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadCiliumCli() automa.Builder {
	ciliumFile := fmt.Sprintf("cilium-%s-%s.tar.gz", b.OS, b.ARCH)
	return automa_steps.NewBashScriptStep(DownloadCiliumStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/cilium/cilium-cli/releases/download/v%s/%s",
			ciliumFile, b.CiliumCliVersion, ciliumFile),
		fmt.Sprintf("curl -sSLo %s.sha256sum https://github.com/cilium/cilium-cli/releases/download/v%s/%s.sha256sum",
			ciliumFile, b.CiliumCliVersion, ciliumFile),
		fmt.Sprintf("sha256sum -c %s.sha256sum", ciliumFile),
	}, fmt.Sprintf("%s/cilium", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) SetupSandboxFolders() automa.Builder {
	return automa_steps.NewBashScriptStep(SetupSandboxFoldersStepId, []string{
		fmt.Sprintf("sudo mkdir -p %s/utils", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/crio/unpack", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/kubernetes", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/cilium", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/bin", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s/logs", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo mkdir -p %s", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/bin", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/crio/keys", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/default", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/sysconfig", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/provisioner", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/containers/registries.conf.d", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/cni/net.d", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/nri/conf.d", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/etc/kubernetes/pki", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/etcd", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/containers/storage", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/kubelet", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/lib/crio", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/cilium", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/nri", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/containers/storage", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/run/crio/exits", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/var/logs/crio/pods", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/run/runc", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/libexec/crio", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/lib/systemd/system/kubelet.service.d", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/bin", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/man", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/oci-umount/oci-umount.d", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/bash-completion/completions", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/fish/completions", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/local/share/zsh/site-functions", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/opt/cni/bin", b.SandboxDir),
		fmt.Sprintf("sudo mkdir -p %s/opt/nri/plugins", b.SandboxDir),
		fmt.Sprintf("sudo chown -R %d:%d %s", b.UID, b.GID, b.ProvisionerHomeDir),
		fmt.Sprintf("sudo chown -R root:root %s", b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) SetupBindMounts() automa.Builder {
	return automa_steps.NewBashScriptStep(SetupBindMountsStepId, []string{
		"sudo mkdir -p /etc/kubernetes /var/lib/kubelet /var/run/cilium",
		fmt.Sprintf("if ! grep -q '/etc/kubernetes' /etc/fstab; then echo '%s/etc/kubernetes /etc/kubernetes none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", b.SandboxDir),
		fmt.Sprintf("if ! grep -q '/var/lib/kubelet' /etc/fstab; then echo '%s/var/lib/kubelet /var/lib/kubelet none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", b.SandboxDir),
		fmt.Sprintf("if ! grep -q '/var/run/cilium' /etc/fstab; then echo '%s/var/run/cilium /var/run/cilium none bind,nofail 0 0' | sudo tee -a /etc/fstab >/dev/null; fi", b.SandboxDir),
		"sudo systemctl daemon-reload",
		"sudo mount /etc/kubernetes",
		"sudo mount /var/lib/kubelet",
		"sudo mount /var/run/cilium",
	}, "")
}

func (b *bashScriptStep) InstallDasel() automa.Builder {
	daselFile := "dasel_" + b.OS + "_" + b.ARCH
	return automa_steps.NewBashScriptStep(InstallDaselStepId, []string{
		fmt.Sprintf("sudo install -m 755 %s %s/dasel", daselFile, b.SandboxBinDir),
	}, fmt.Sprintf("%s/utils", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", b.ARCH, b.CrioVersion)
	return automa_steps.NewBashScriptStep(InstallCrioStepId, []string{
		fmt.Sprintf("sudo tar -C %s/crio/unpack -zxvf %s", b.ProvisionerHomeDir, crioFile),
		fmt.Sprintf(`pushd %s/crio/unpack/cri-o >/dev/null 2>&1 || true;
		DESTDIR=%s SYSTEMDDIR=/usr/lib/systemd/system sudo -E bash ./install; 
		popd >/dev/null 2>&1 || true`, b.ProvisionerHomeDir, b.SandboxDir),
	}, fmt.Sprintf("%s/crio", b.ProvisionerHomeDir))
}

// InstallKubernetesTools installs kubeadm, kubelet, kubectl, k9s, helm, cilium-cli and sets up the
// TODO: split this step into separate steps
func (b *bashScriptStep) InstallKubernetesTools() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallKubernetesToolsStepId, []string{
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubeadm\" \"%s/kubeadm\"", b.ProvisionerHomeDir, b.SandboxBinDir),
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubelet\" \"%s/kubelet\"", b.ProvisionerHomeDir, b.SandboxBinDir),
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubectl\" \"%s/kubectl\"", b.ProvisionerHomeDir, b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubeadm\" /usr/local/bin/kubeadm", b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubelet\" /usr/local/bin/kubelet", b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubectl\" /usr/local/bin/kubectl", b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/k9s\" /usr/local/bin/k9s", b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/helm\" /usr/local/bin/helm", b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/cilium\" /usr/local/bin/cilium", b.SandboxBinDir),
		fmt.Sprintf("sudo mkdir -p %s/usr/lib/systemd/system/kubelet.service.d", b.SandboxDir),
		fmt.Sprintf("sudo cp \"%s/kubernetes/kubelet.service\" \"%s/usr/lib/systemd/system/kubelet.service\"", b.ProvisionerHomeDir, b.SandboxDir),
		fmt.Sprintf("sudo cp \"%s/kubernetes/10-kubeadm.conf\" \"%s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf\"", b.ProvisionerHomeDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) ConfigureSandboxKubeletService() automa.Builder {
	return automa_steps.NewBashScriptStep(ConfigureSandboxKubeletSvcStepId, []string{
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service",
			b.SandboxBinDir, b.SandboxDir),
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf",
			b.SandboxBinDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) ConfigureSandboxCrio() automa.Builder {
	return automa.NewWorkFlowBuilder(ConfigureSandboxCrioStepId).Steps(
		automa_steps.NewBashScriptStep("symlink-sandbox-crio", []string{
			fmt.Sprintf("sudo ln -sf %s/etc/containers /etc/containers", b.SandboxDir),
		}, ""),
		b.ConfigureSandboxCrioDefaults(),
		b.ConfigureSandboxCrioService(),
		b.UpdateCrioConfiguration(),
		b.SetupCrioServiceSymlinks(),
	)
}

func (b *bashScriptStep) ConfigureSandboxCrioService() automa.Builder {
	return automa_steps.NewBashScriptStep(ConfigureSandboxCrioSvcStepId, []string{
		fmt.Sprintf("sudo sed -i 's|/usr/local/bin/crio|%s/crio|' %s/usr/lib/systemd/system/crio.service",
			b.SandboxLocalBinDir, b.SandboxDir),
		fmt.Sprintf("sudo sed -i 's|-/etc/sysconfig/crio|%s/etc/default/crio|' %s/usr/lib/systemd/system/crio.service",
			b.SandboxDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) ConfigureSandboxCrioDefaults() automa.Builder {
	return automa_steps.NewBashScriptStep(ConfigureSandboxCrioDefaultsStepId, []string{
		fmt.Sprintf(`cat <<EOF | sudo tee %s/etc/default/crio >/dev/null
# /etc/default/crio

# use "--enable-metrics" and "--metrics-port value"
#CRIO_METRICS_OPTIONS="--enable-metrics"

#CRIO_NETWORK_OPTIONS=
#CRIO_STORAGE_OPTIONS=

# CRI-O configuration directory
CRIO_CONFIG_OPTIONS="--config-dir=%s/etc/crio/crio.conf.d"
EOF`, b.SandboxDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) UpdateCrioConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep(UpdateCrioConfStepId, []string{
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v runc '.crio.runtime.default_runtime'", b.SandboxBinDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/etc/crio/keys\" '.crio.runtime.decryption_keys_path'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/exits\" '.crio.runtime.container_exits_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio\" '.crio.runtime.container_attach_socket_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run\" '.crio.runtime.namespaces_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/pinns\" '.crio.runtime.pinns_path'", b.SandboxBinDir, b.SandboxDir, b.SandboxLocalBinDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/run/runc\" '.crio.runtime.runtimes.runc.runtime_root'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/crio.sock\" '.crio.api.listen'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/crio.sock\" '.crio.api.listen'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/lib/containers/storage\" '.crio.root'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/containers/storage\" '.crio.runroot'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/crio/version\" '.crio.version_file'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/logs/crio/pods\" '.crio.log_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/lib/crio/clean.shutdown\" '.crio.clean_shutdown_file'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/etc/cni/net.d/\" '.crio.network.network_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/opt/cni/bin\" -s 'crio.network.plugin_dirs.[]'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/opt/nri/plugins\" '.crio.nri.nri_plugin_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/etc/nri/conf.d\" '.crio.nri.nri_plugin_config_dir'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
		fmt.Sprintf("sudo %s/dasel put -w toml -r toml -f \"%s/etc/crio/crio.conf.d/10-crio.conf\" -v \"%s/var/run/nri/nri.sock\" '.crio.nri.nri_listen'", b.SandboxBinDir, b.SandboxDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) SetupCrioServiceSymlinks() automa.Builder {
	return automa_steps.NewBashScriptStep(SetupCrioSymlinkStepId, []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service", b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) InstallK9s() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallK9sStepId, []string{
		fmt.Sprintf("sudo tar -C %s -zxvf k9s_%s_%s.tar.gz k9s", b.SandboxBinDir, b.OS, b.ARCH),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallHelm() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallHelmStepId, []string{
		fmt.Sprintf("sudo tar -C %s -zxvf helm-v%s-%s-%s.tar.gz %s-%s/helm --strip-components 1",
			b.SandboxBinDir, b.HelmVersion, b.OS, b.ARCH, b.OS, b.ARCH),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallCiliumCli() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallCiliumStepId, []string{
		fmt.Sprintf("sudo tar -C %s -zxvf cilium-%s-%s.tar.gz", b.SandboxBinDir, b.OS, b.ARCH),
	}, fmt.Sprintf("%s/cilium", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) SetupSystemdServiceSymlinks() automa.Builder {
	return automa_steps.NewBashScriptStep(SetupSystemdServiceSymlinksStepId, []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service /usr/lib/systemd/system/kubelet.service", b.SandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service.d /usr/lib/systemd/system/kubelet.service.d", b.SandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service", b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) EnableAndStartServices() automa.Builder {
	return automa_steps.NewBashScriptStep(EnableStartServicesStepId, []string{
		"sudo systemctl daemon-reload",
		"sudo systemctl enable crio kubelet",
		"sudo systemctl start crio kubelet",
	}, "")
}

func (b *bashScriptStep) TorchPriorKubeAdmConfiguration() automa.Builder {
	return automa_steps.NewBashScriptStep(TorchPriorKubeAdmConfigStepId, []string{
		fmt.Sprintf("sudo %s/kubeadm reset --force || true", b.SandboxBinDir),
		fmt.Sprintf("sudo rm -rf %s/etc/kubernetes/* %s/etc/cni/net.d/* %s/var/lib/etcd/* || true",
			b.SandboxDir, b.SandboxDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) SetupKubeAdminConfiguration() automa.Builder {
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
KubernetesVersion: "%s"
networking:
  dnsDomain: cluster.local
  serviceSubnet: 10.0.0.0/14
  podSubnet: 10.4.0.0/14
controllerManager:
  extraArgs:
    - name: node-cidr-mask-size-ipv4
      value: "24"
EOF`, b.SandboxDir, b.KubeBootstrapToken, b.MachineIp, b.SandboxDir, b.Hostname, b.MachineIp, b.MachineIp,
			b.SandboxDir, b.SandboxDir, b.KubernetesVersion)

	return automa_steps.NewBashScriptStep(SetupKubeAdminConfigStepId, []string{
		configScript,
	}, "")
}

func (b *bashScriptStep) SetPipeFailMode() automa.Builder {
	return automa_steps.NewBashScriptStep(SetBashPipeFailModeStepId, []string{
		"set -eo pipefail",
	}, "")
}

func (b *bashScriptStep) InitializeKubernetesCluster() automa.Builder {
	return automa_steps.NewBashScriptStep(InitializeKubernetesClusterStepId, []string{
		fmt.Sprintf("sudo %s/kubeadm init --upload-certs --config %s/etc/provisioner/kubeadm-init.yaml",
			b.SandboxBinDir, b.SandboxDir),
		fmt.Sprintf("mkdir -p \"%s/.kube\"", b.HomeDir),
		fmt.Sprintf("sudo cp -f %s/etc/kubernetes/admin.conf \"%s/.kube/config\"",
			b.SandboxDir, b.HomeDir),
		fmt.Sprintf("sudo chown \"%d:%d\" \"%s/.kube/config\"", b.UID, b.GID, b.HomeDir),
	}, "")
}

func (b *bashScriptStep) ConfigureCiliumCNI() automa.Builder {
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
EOF`, b.SandboxDir, b.MachineIp, b.SandboxDir, b.SandboxDir, b.SandboxDir)
	return automa_steps.NewBashScriptStep(ConfigureCiliumCNIStepId, []string{
		configScript,
	}, "")
}

func (b *bashScriptStep) InstallCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallCiliumCNIStepId, []string{
		fmt.Sprintf("sudo %s/cilium install --version \"%s\" --values %s/etc/provisioner/cilium-config.yaml",
			b.SandboxBinDir, b.CiliumVersion, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) EnforceCiliumCNI() automa.Builder {
	return automa_steps.NewBashScriptStep(EnforceCiliumCNIStepId, []string{
		"sudo sysctl --system >/dev/null",
		"sudo systemctl restart kubelet crio",
		fmt.Sprintf("%s/cilium status --wait", b.SandboxBinDir),
	}, "")
}

func (b *bashScriptStep) InstallMetalLB() automa.Builder {
	return automa_steps.NewBashScriptStep(InstallMetalLBStepId, []string{
		fmt.Sprintf("sudo %s/helm repo add metallb https://metallb.github.io/metallb", b.SandboxBinDir),
		fmt.Sprintf("sudo %s/helm install metallb metallb/metallb --version %s \\\n"+
			"--set speaker.frr.enabled=false \\\n"+
			"--namespace metallb-system --create-namespace --atomic --wait",
			b.SandboxBinDir, b.MetallbVersion),
		"sleep 60",
	}, "")
}

func (b *bashScriptStep) DeployMetallbConfiguration() automa.Builder {
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
EOF`, b.SandboxBinDir, b.MachineIp)
	return automa_steps.NewBashScriptStep(DeployMetallbConfigStepId, []string{
		configScript,
	}, "")
}

func initBashScriptBasedStepRegistry() (automa.Registry, error) {
	r := automa.NewRegistry()
	err := r.Add(
		bashSteps.SetupSandboxFolders(),
		bashSteps.UpdateOS(),
		bashSteps.DisableSwap(),
		bashSteps.RemoveContainerd(),
		bashSteps.InstallGnupg2(),
		bashSteps.InstallConntrack(),
		bashSteps.InstallSoCat(),
		bashSteps.InstallEBTables(),
		bashSteps.InstallNFTables(),
		bashSteps.EnableAndStartNFTables(),
		bashSteps.AutoRemovePackages(),
		bashSteps.InstallKernelModules(),
		bashSteps.ConfigureKernelModules(),
		bashSteps.DownloadDasel(),
		bashSteps.DownloadCrio(),
		bashSteps.DownloadKubernetesTools(),
		bashSteps.DownloadCiliumCli(),
		bashSteps.SetupBindMounts(),
		bashSteps.InstallDasel(),
		bashSteps.InstallCrio(),
		bashSteps.InstallKubernetesTools(),
		bashSteps.ConfigureSandboxKubeletService(),
		bashSteps.ConfigureSandboxCrio(),
		bashSteps.InstallK9s(),
		bashSteps.InstallHelm(),
		bashSteps.InstallCiliumCli(),
		bashSteps.SetupSystemdServiceSymlinks(),
		bashSteps.EnableAndStartServices(),
		bashSteps.TorchPriorKubeAdmConfiguration(),
		bashSteps.SetupKubeAdminConfiguration(),
		bashSteps.SetPipeFailMode(),
		bashSteps.InitializeKubernetesCluster(),
		bashSteps.ConfigureCiliumCNI(),
		bashSteps.InstallCiliumCNI(),
		bashSteps.EnforceCiliumCNI(),
		bashSteps.InstallMetalLB(),
		bashSteps.DeployMetallbConfiguration(),
		BashScriptBasedClusterSetupWorkflow(), // add the workflow as well
	)

	if err != nil {
		return nil, err
	}

	return r, nil
}

// BashScriptBasedStepRegistry returns a registry containing all bash script based steps
// External callers should use the registry to access various Steps as required rather than attempting to access
// bashScriptStep struct or bashSteps directly
func BashScriptBasedStepRegistry() automa.Registry {
	return bashStepsRegistry
}

// BashScriptBasedClusterSetupWorkflow returns a workflow that sets up a Kubernetes cluster using bash scripts
func BashScriptBasedClusterSetupWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("setup-kubernetes-cluster").Steps(
		bashSteps.SetupSandboxFolders(),
		bashSteps.UpdateOS(),
		bashSteps.DisableSwap(),
		bashSteps.RemoveContainerd(),
		bashSteps.InstallGnupg2(),
		bashSteps.InstallConntrack(),
		bashSteps.InstallSoCat(),
		bashSteps.InstallEBTables(),
		bashSteps.InstallNFTables(),
		bashSteps.EnableAndStartNFTables(),
		bashSteps.AutoRemovePackages(),
		bashSteps.InstallKernelModules(),
		bashSteps.ConfigureKernelModules(),
		bashSteps.SetupBindMounts(),
		bashSteps.DownloadDasel(),
		bashSteps.InstallDasel(),
		bashSteps.DownloadKubernetesTools(),
		bashSteps.InstallKubernetesTools(),
		bashSteps.InstallK9s(),
		bashSteps.InstallHelm(),
		bashSteps.DownloadCrio(),
		bashSteps.InstallCrio(),
		bashSteps.ConfigureSandboxCrio(),
		bashSteps.DownloadCiliumCli(),
		bashSteps.InstallCiliumCli(),
		bashSteps.ConfigureSandboxKubeletService(),
		bashSteps.SetupSystemdServiceSymlinks(),
		bashSteps.EnableAndStartServices(),
		bashSteps.TorchPriorKubeAdmConfiguration(),
		bashSteps.SetupKubeAdminConfiguration(),
		bashSteps.SetPipeFailMode(),
		bashSteps.InitializeKubernetesCluster(),
		bashSteps.ConfigureCiliumCNI(),
		bashSteps.InstallCiliumCNI(),
		bashSteps.EnforceCiliumCNI(),
		bashSteps.InstallMetalLB(),
		bashSteps.DeployMetallbConfiguration(),
	)
}
