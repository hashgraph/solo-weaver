package steps

import (
	"fmt"
	"os"
	"runtime"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/internal/core"
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

	EnableStartNFTablesStepId  = "enable-start-nftables"
	AutoRemovePackagesStepId   = "auto-remove-packages"
	InstallKernelModulesStepId = "install-kernel-modules"
	SetupWorkingDirsStepId     = "setup-working-dirs"

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

	InstallDaselStepId   = "install-dasel"
	InstallCrioStepId    = "install-crio"
	InstallKubeadmStepId = "install-kubeadm"
	InstallKubeletStepId = "install-kubelet"
	InstallKubectlStepId = "install-kubectl"

	InstallK9sStepId       = "install-k9s"
	InstallHelmStepId      = "install-helm"
	InstallCiliumStepId    = "install-cilium"
	InstallCiliumCNIStepId = "install-cilium-cni"
	InstallMetalLBStepId   = "install-metallb"

	ConfigureKubeadmStepId = "configure-kubeadm"
	ConfigureKubeletStepId = "configure-kubelet"

	ConfigureSandboxKubeletSvcStepId   = "configure-kubelet-service"
	ConfigureSandboxCrioStepId         = "configure-sandbox-crio"
	ConfigureSandboxCrioSvcStepId      = "configure-sandbox-crio-service"
	ConfigureSandboxCrioDefaultsStepId = "configure-sandbox-crio-default"
	UpdateCrioConfStepId               = "update-crio-configuration"
	SetupCrioSymlinkStepId             = "setup-crio-symlinks"
	SetupSystemdServiceSymlinksStepId  = "setup-systemd-symlinks"
	EnableStartCrioStepId              = "enable-start-crio"
	EnableStartKubeletStepId           = "enable-start-kubelet"
	DaemonReloadStepId                 = "daemon-reload-step"
	TorchPriorKubeAdmConfigStepId      = "torch-prior-kubeadm-config"
	ConfigureKubeadmInitStepId         = "configure-kubeadm-init"
	SetBashPipeFailModeStepId          = "set-bash-pipefail-mode"
	InitClusterStepId                  = "init-cluster"
	ConfigureCiliumCNIStepId           = "configure-cilium-cni"
	EnforceCiliumCNIStepId             = "restart-crio-and-kubelet"
	DeployMetallbConfigStepId          = "deploy-metallb-config"
	CheckClusterHealthStepId           = "check-cluster-health"
	SetupClusterStepId                 = "setup-kubernetes-cluster"
)

// initialized in init()
var (
	bashSteps bashScriptStep
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
	machineIp, err := runCmd(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)
	if err != nil {
		machineIp = "0.0.0.0"
		logx.As().Warn().Err(err).Str("machine_ip", machineIp).
			Msg("failed to get machine IP, defaulting to 0.0.0.0")
	}

	hostname, err := runCmd("hostname")
	if err != nil {
		hostname = "localhost"
		logx.As().Warn().Err(err).Str("localhost", hostname).
			Msg("failed to get hostname, defaulting to localhost")
	}

	kubeBootstrapToken, err := generateKubeadmToken()
	if err != nil {
		kubeBootstrapToken = "abcdef.0123456789abcdef"
		logx.As().Warn().Err(err).Str("token", kubeBootstrapToken).
			Msg("failed to generate kubeadm token, defaulting to a static token: abcdef.0123456789abcdef")
	}

	return bashScriptStep{
		HomeDir:            os.Getenv("HOME"),
		OS:                 runtime.GOOS,
		ARCH:               runtime.GOARCH,
		UID:                os.Geteuid(),
		GID:                os.Getgid(),
		ProvisionerHomeDir: core.Paths().HomeDir,
		SandboxDir:         core.Paths().SandboxDir,
		SandboxBinDir:      core.Paths().SandboxBinDir,
		SandboxLocalBinDir: core.Paths().SandboxLocalBinDir,
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
	return automa_steps.BashScriptStep(UpdateOSStepId, []string{
		"sudo apt-get update -y",
	}, "")
}

func (b *bashScriptStep) DisableSwap() automa.Builder {
	return automa_steps.BashScriptStep(DisableSwapStepId, []string{
		`sudo sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab`,
		`sudo swapoff -a`,
	}, "")
}

func (b *bashScriptStep) RemoveContainerd() automa.Builder {
	return automa_steps.BashScriptStep(RemoveContainerdStepId, []string{
		"sudo apt-get remove -y containerd containerd.io || true",
	}, "")
}

func (b *bashScriptStep) InstallIPTables() automa.Builder {
	return automa_steps.BashScriptStep(InstallIPTablesStepId, []string{
		"sudo apt-get install -y iptables",
	}, "")
}

func (b *bashScriptStep) InstallGnupg2() automa.Builder {
	return automa_steps.BashScriptStep(InstallGnupg2StepId, []string{
		"sudo apt-get install -y gnupg2",
	}, "")
}

func (b *bashScriptStep) InstallConntrack() automa.Builder {
	return automa_steps.BashScriptStep(InstallConntrackStepId, []string{
		"sudo apt-get install -y conntrack",
	}, "")
}

func (b *bashScriptStep) InstallSoCat() automa.Builder {
	return automa_steps.BashScriptStep(InstallSoCatStepId, []string{
		"sudo apt-get install -y socat",
	}, "")
}

func (b *bashScriptStep) InstallEBTables() automa.Builder {
	return automa_steps.BashScriptStep(InstallEBTablesStepId, []string{
		"sudo apt-get install -y ebtables",
	}, "")
}

func (b *bashScriptStep) InstallNFTables() automa.Builder {
	return automa_steps.BashScriptStep(InstallNFTablesStepId, []string{
		"sudo apt-get install -y nftables",
	}, "")
}

func (b *bashScriptStep) EnableAndStartNFTables() automa.Builder {
	return automa_steps.BashScriptStep(EnableStartNFTablesStepId, []string{
		"sudo systemctl enable nftables",
		"sudo systemctl start nftables",
	}, "")
}

func (b *bashScriptStep) AutoRemovePackages() automa.Builder {
	return automa_steps.BashScriptStep(AutoRemovePackagesStepId, []string{
		"sudo apt-get autoremove -y",
	}, "")
}

func (b *bashScriptStep) InstallKernelModules() automa.Builder {
	return automa_steps.BashScriptStep(InstallKernelModulesStepId, []string{
		"sudo modprobe overlay",
		"sudo modprobe br_netfilter",
		`echo 'overlay' | sudo tee /etc/modules-load.d/overlay.conf`,
		`echo 'br_netfilter' | sudo tee /etc/modules-load.d/br_netfilter.conf`,
	}, "")
}

func (b *bashScriptStep) ConfigureSysctlForKubernetes() automa.Builder {
	return automa.NewWorkflowBuilder().
		WithId(ConfigureSysctlForKubernetesStepId).
		Steps(
			automa_steps.BashScriptStep("cleanup-sysctl-configs", []string{
				`sudo rm -f /etc/sysctl.d/15-network-performance.conf || true`,
				`sudo rm -f /etc/sysctl.d/15-k8s-networking.conf || true`,
				`sudo rm -f /etc/sysctl.d/15-inotify.conf || true`,
			}, ""),
			automa_steps.BashScriptStep("configure-kernel-modules", []string{
				`echo 'net.bridge.bridge-nf-call-iptables = 1' | sudo tee /etc/sysctl.d/99-kubernetes-cri.conf`,
				`echo 'net.ipv4.ip_forward = 1' | sudo tee -a /etc/sysctl.d/99-kubernetes-cri.conf`,
				`echo 'net.bridge.bridge-nf-call-ip6tables = 1' | sudo tee -a /etc/sysctl.d/99-kubernetes-cri.conf`,
				"sudo sysctl --system >/dev/null",
			}, ""),
			automa_steps.BashScriptStep("create-k8s-networking-config", []string{
				`cat <<EOF | sudo tee /etc/sysctl.d/75-k8s-networking.conf
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
EOF`,
			}, ""),
			automa_steps.BashScriptStep("create-network-performance-config", []string{
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
			automa_steps.BashScriptStep("create-inotify-config", []string{
				`cat <<EOF | sudo tee /etc/sysctl.d/75-inotify.conf
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 512
EOF`,
			}, ""),
			automa_steps.BashScriptStep("reload-sysctl", []string{
				"sudo sysctl --system >/dev/null",
			}, ""),
		)
}

func (b *bashScriptStep) DownloadDasel() automa.Builder {
	daselFile := "dasel_" + b.OS + "_" + b.ARCH
	return automa_steps.BashScriptStep(DownloadDaselStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/TomWright/dasel/releases/download/v%s/%s",
			daselFile, b.DaselVersion, daselFile),
	}, fmt.Sprintf("%s/utils", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", b.ARCH, b.CrioVersion)
	return automa_steps.BashScriptStep(DownloadCrioStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://storage.googleapis.com/cri-o/artifacts/%s",
			crioFile, crioFile),
	}, fmt.Sprintf("%s/crio", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubeadm() automa.Builder {
	return automa_steps.BashScriptStep(DownloadKubeAdmStepId, []string{
		fmt.Sprintf("curl -sSLo kubeadm https://dl.k8s.io/release/v%s/bin/%s/%s/kubeadm",
			b.KubernetesVersion, b.OS, b.ARCH),
		"sudo chmod +x kubeadm",
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubelet() automa.Builder {
	return automa_steps.BashScriptStep(DownloadKubeletStepId, []string{
		fmt.Sprintf("curl -sSLo kubelet https://dl.k8s.io/release/v%s/bin/%s/%s/kubelet",
			b.KubernetesVersion, b.OS, b.ARCH),
		"sudo chmod +x kubelet",
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubectl() automa.Builder {
	return automa_steps.BashScriptStep(DownloadKubectlStepId, []string{
		fmt.Sprintf("curl -sSLo kubectl https://dl.k8s.io/release/v%s/bin/%s/%s/kubectl",
			b.KubernetesVersion, b.OS, b.ARCH),
		"sudo chmod +x kubectl",
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadK9s() automa.Builder {
	k9sFile := fmt.Sprintf("k9s_%s_%s.tar.gz", b.OS, b.ARCH)
	return automa_steps.BashScriptStep(DownloadK9sStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/derailed/k9s/releases/download/v%s/%s",
			k9sFile, b.K9sVersion, k9sFile),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadHelm() automa.Builder {
	helmFile := fmt.Sprintf("helm-v%s-%s-%s.tar.gz", b.HelmVersion, b.OS, b.ARCH)
	return automa_steps.BashScriptStep(DownloadHelmStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://get.helm.sh/%s",
			helmFile, helmFile),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubeletConfig() automa.Builder {
	return automa_steps.BashScriptStep(DownloadKubernetesServiceFilesStepId, []string{
		fmt.Sprintf("curl -sSLo kubelet.service https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubelet/kubelet.service",
			b.KrelVersion),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadKubeadmConfig() automa.Builder {
	return automa_steps.BashScriptStep(DownloadKubernetesServiceFilesStepId, []string{
		fmt.Sprintf("curl -sSLo 10-kubeadm.conf https://raw.githubusercontent.com/kubernetes/release/%s/cmd/krel/templates/latest/kubeadm/10-kubeadm.conf",
			b.KrelVersion),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) DownloadCiliumCli() automa.Builder {
	ciliumFile := fmt.Sprintf("cilium-%s-%s.tar.gz", b.OS, b.ARCH)
	return automa_steps.BashScriptStep(DownloadCiliumStepId, []string{
		fmt.Sprintf("curl -sSLo %s https://github.com/cilium/cilium-cli/releases/download/v%s/%s",
			ciliumFile, b.CiliumCliVersion, ciliumFile),
		fmt.Sprintf("curl -sSLo %s.sha256sum https://github.com/cilium/cilium-cli/releases/download/v%s/%s.sha256sum",
			ciliumFile, b.CiliumCliVersion, ciliumFile),
		fmt.Sprintf("sha256sum -c %s.sha256sum", ciliumFile),
	}, fmt.Sprintf("%s/cilium", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) SetupSandboxFolders() automa.Builder {
	return automa_steps.BashScriptStep(SetupSandboxFoldersStepId, []string{
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
	return automa_steps.BashScriptStep(SetupBindMountsStepId, []string{
		"sudo mkdir -p /etc/kubernetes /var/lib/kubelet /var/run/cilium || true",
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
	return automa_steps.BashScriptStep(InstallDaselStepId, []string{
		fmt.Sprintf("sudo install -m 755 %s %s/dasel", daselFile, b.SandboxBinDir),
	}, fmt.Sprintf("%s/utils", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallCrio() automa.Builder {
	crioFile := fmt.Sprintf("cri-o.%s.v%s.tar.gz", b.ARCH, b.CrioVersion)
	return automa_steps.BashScriptStep(InstallCrioStepId, []string{
		fmt.Sprintf("sudo tar -C %s/crio/unpack -zxvf %s", b.ProvisionerHomeDir, crioFile),
		fmt.Sprintf(`pushd %s/crio/unpack/cri-o >/dev/null 2>&1 || true;
		DESTDIR=%s SYSTEMDDIR=/usr/lib/systemd/system sudo -E bash ./install; 
		popd >/dev/null 2>&1 || true`, b.ProvisionerHomeDir, b.SandboxDir),
	}, fmt.Sprintf("%s/crio", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallKubeadm() automa.Builder {
	return automa_steps.BashScriptStep(InstallKubeadmStepId, []string{
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubeadm\" \"%s/kubeadm\"", b.ProvisionerHomeDir, b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubeadm\" /usr/local/bin/kubeadm", b.SandboxBinDir),
	}, "")
}

func (b *bashScriptStep) ConfigureKubeadm() automa.Builder {
	return automa.NewWorkflowBuilder().WithId(ConfigureKubeadmStepId).Steps(
		automa_steps.BashScriptStep("install-kubeadm-conf", []string{
			fmt.Sprintf("sudo mkdir -p %s/usr/lib/systemd/system/kubelet.service.d || true", b.SandboxDir),
			fmt.Sprintf("sudo cp \"%s/kubernetes/10-kubeadm.conf\" \"%s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf\"", b.ProvisionerHomeDir, b.SandboxDir),
			fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf", b.SandboxBinDir, b.SandboxDir),
			fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service.d /usr/lib/systemd/system/kubelet.service.d", b.SandboxDir),
		}, ""),
		b.ConfigureKubeadmInit(),
	)
}

func (b *bashScriptStep) InstallKubelet() automa.Builder {
	return automa_steps.BashScriptStep(InstallKubeletStepId, []string{
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubelet\" \"%s/kubelet\"", b.ProvisionerHomeDir, b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubelet\" /usr/local/bin/kubelet", b.SandboxBinDir),
	}, "")
}

func (b *bashScriptStep) ConfigureKubelet() automa.Builder {
	return automa_steps.BashScriptStep(ConfigureKubeletStepId, []string{
		fmt.Sprintf("sudo cp \"%s/kubernetes/kubelet.service\" \"%s/usr/lib/systemd/system/kubelet.service\"", b.ProvisionerHomeDir, b.SandboxDir),
		fmt.Sprintf("sudo sed -i 's|/usr/bin/kubelet|%s/kubelet|' %s/usr/lib/systemd/system/kubelet.service", b.SandboxBinDir, b.SandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service /usr/lib/systemd/system/kubelet.service", b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) InstallKubectl() automa.Builder {
	return automa_steps.BashScriptStep(InstallKubeletStepId, []string{
		fmt.Sprintf("sudo mkdir -p \"%s/kubernetes/kubectl\" || true", b.ProvisionerHomeDir),
		fmt.Sprintf("sudo install -m 755 \"%s/kubernetes/kubectl\" \"%s/kubectl\"", b.ProvisionerHomeDir, b.SandboxBinDir),
		fmt.Sprintf("sudo ln -sf \"%s/kubectl\" /usr/local/bin/kubectl", b.SandboxBinDir),
	}, "")
}

func (b *bashScriptStep) ConfigureSandboxCrio() automa.Builder {
	return automa.NewWorkflowBuilder().
		WithId(ConfigureSandboxCrioStepId).
		Steps(
			automa_steps.BashScriptStep("symlink-sandbox-crio", []string{
				fmt.Sprintf("sudo ln -sf %s/etc/containers /etc/containers", b.SandboxDir),
			}, ""),
			b.ConfigureSandboxCrioDefaults(),
			b.ConfigureSandboxCrioService(),
			b.UpdateCrioConfiguration(),
			b.SetupCrioServiceSymlinks(),
		)
}

func (b *bashScriptStep) ConfigureSandboxCrioService() automa.Builder {
	return automa_steps.BashScriptStep(ConfigureSandboxCrioSvcStepId, []string{
		fmt.Sprintf("sudo sed -i 's|/usr/local/bin/crio|%s/crio|' %s/usr/lib/systemd/system/crio.service",
			b.SandboxLocalBinDir, b.SandboxDir),
		fmt.Sprintf("sudo sed -i 's|-/etc/sysconfig/crio|%s/etc/default/crio|' %s/usr/lib/systemd/system/crio.service",
			b.SandboxDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) ConfigureSandboxCrioDefaults() automa.Builder {
	return automa_steps.BashScriptStep(ConfigureSandboxCrioDefaultsStepId, []string{
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
	return automa_steps.BashScriptStep(UpdateCrioConfStepId, []string{
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
	return automa_steps.BashScriptStep(SetupCrioSymlinkStepId, []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/crio.service /usr/lib/systemd/system/crio.service", b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) InstallK9s() automa.Builder {
	return automa_steps.BashScriptStep(InstallK9sStepId, []string{
		fmt.Sprintf("sudo tar -C %s -zxvf k9s_%s_%s.tar.gz k9s", b.SandboxBinDir, b.OS, b.ARCH),
		fmt.Sprintf("sudo ln -sf \"%s/k9s\" /usr/local/bin/k9s", b.SandboxBinDir),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallHelm() automa.Builder {
	return automa_steps.BashScriptStep(InstallHelmStepId, []string{
		fmt.Sprintf("sudo tar -C %s -zxvf helm-v%s-%s-%s.tar.gz %s-%s/helm --strip-components 1",
			b.SandboxBinDir, b.HelmVersion, b.OS, b.ARCH, b.OS, b.ARCH),
		fmt.Sprintf("sudo ln -sf \"%s/helm\" /usr/local/bin/helm", b.SandboxBinDir),
	}, fmt.Sprintf("%s/kubernetes", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) InstallCiliumCli() automa.Builder {
	return automa_steps.BashScriptStep(InstallCiliumStepId, []string{
		fmt.Sprintf("sudo tar -C %s -zxvf cilium-%s-%s.tar.gz", b.SandboxBinDir, b.OS, b.ARCH),
		fmt.Sprintf("sudo ln -sf \"%s/cilium\" /usr/local/bin/cilium", b.SandboxBinDir),
	}, fmt.Sprintf("%s/cilium", b.ProvisionerHomeDir))
}

func (b *bashScriptStep) SetupSystemdServiceSymlinks() automa.Builder {
	return automa_steps.BashScriptStep(SetupSystemdServiceSymlinksStepId, []string{
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service /usr/lib/systemd/system/kubelet.service", b.SandboxDir),
		fmt.Sprintf("sudo ln -sf %s/usr/lib/systemd/system/kubelet.service.d /usr/lib/systemd/system/kubelet.service.d", b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) DaemonReload() automa.Builder {
	return automa_steps.BashScriptStep(DaemonReloadStepId, []string{
		"sudo systemctl daemon-reload",
	}, "")
}

func (b *bashScriptStep) EnableAndStartCrio() automa.Builder {
	return automa_steps.BashScriptStep(EnableStartCrioStepId, []string{
		"sudo systemctl daemon-reload",
		"sudo systemctl enable crio",
		"sudo systemctl start crio",
	}, "")
}

func (b *bashScriptStep) EnableAndStartKubelet() automa.Builder {
	return automa_steps.BashScriptStep(EnableStartKubeletStepId, []string{
		"sudo systemctl daemon-reload",
		"sudo systemctl enable kubelet",
		"sudo systemctl start kubelet",
	}, "")
}

func (b *bashScriptStep) TorchPriorKubeAdmConfiguration() automa.Builder {
	return automa_steps.BashScriptStep(TorchPriorKubeAdmConfigStepId, []string{
		fmt.Sprintf(`sudo kill -9 $(sudo lsof -t -i :6443) || true; sleep 5;`), // Kill any process using port 6443 so that kubeadm reset can complete
		fmt.Sprintf("sudo %s/kubeadm reset --force || true", b.SandboxBinDir),
		fmt.Sprintf("sudo rm -rf %s/etc/kubernetes/* %s/etc/cni/net.d/* %s/var/lib/etcd/* || true",
			b.SandboxDir, b.SandboxDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) ConfigureKubeadmInit() automa.Builder {
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
EOF`, b.SandboxDir, b.KubeBootstrapToken, b.MachineIp, b.SandboxDir, b.Hostname, b.MachineIp, b.MachineIp,
			b.SandboxDir, b.SandboxDir, b.KubernetesVersion)

	return automa_steps.BashScriptStep(ConfigureKubeadmInitStepId, []string{
		configScript,
	}, "")
}

func (b *bashScriptStep) KubeadmConfigImagesPull() automa.Builder {
	return automa_steps.BashScriptStep("kube-config-image-pull", []string{
		fmt.Sprintf("sudo %s/kubeadm config images pull --config %s/etc/provisioner/kubeadm-init.yaml", b.SandboxBinDir, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) InitCluster() automa.Builder {
	return automa.NewWorkflowBuilder().WithId(InitClusterStepId).Steps(
		b.KubeadmConfigImagesPull(),
		automa_steps.BashScriptStep("kubeadm-init", []string{
			fmt.Sprintf("sudo %s/kubeadm init --upload-certs --config %s/etc/provisioner/kubeadm-init.yaml", b.SandboxBinDir, b.SandboxDir),
		}, ""),
	)
}

func (b *bashScriptStep) ConfigureKubeConfig() automa.Builder {
	return automa_steps.BashScriptStep("configure-kube-config-dir", []string{
		fmt.Sprintf("mkdir -p \"%s/.kube\"", b.HomeDir),
		fmt.Sprintf("sudo cp -f %s/etc/kubernetes/admin.conf \"%s/.kube/config\"",
			b.SandboxDir, b.HomeDir),
		fmt.Sprintf("sudo chown \"%d:%d\" \"%s/.kube/config\"", b.UID, b.GID, b.HomeDir),
		fmt.Sprintf("sleep 5"), // wait a bit
	}, "")
}

func (b *bashScriptStep) ConfigureCiliumCNI() automa.Builder {
	configScript := fmt.Sprintf(
		`set -eo pipefail; cat <<EOF | sudo tee %s/etc/provisioner/cilium-config.yaml >/dev/null
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
	return automa_steps.BashScriptStep(ConfigureCiliumCNIStepId, []string{
		configScript,
	}, "")
}

func (b *bashScriptStep) InstallCiliumCNI() automa.Builder {
	return automa_steps.BashScriptStep(InstallCiliumCNIStepId, []string{
		fmt.Sprintf("sudo %s/cilium install --version \"%s\" --values %s/etc/provisioner/cilium-config.yaml",
			b.SandboxBinDir, b.CiliumVersion, b.SandboxDir),
	}, "")
}

func (b *bashScriptStep) EnableAndStartCiliumCNI() automa.Builder {
	return automa_steps.BashScriptStep(EnforceCiliumCNIStepId, []string{
		"sudo sysctl --system >/dev/null",
		"sudo systemctl restart kubelet crio",
		fmt.Sprintf("%s/cilium status --wait", b.SandboxBinDir),
	}, "")
}

func (b *bashScriptStep) InstallMetalLB() automa.Builder {
	return automa_steps.BashScriptStep(InstallMetalLBStepId, []string{
		fmt.Sprintf("sudo %s/helm repo add metallb https://metallb.github.io/metallb", b.SandboxBinDir),
		fmt.Sprintf("sudo %s/helm install metallb metallb/metallb --version %s \\\n"+
			"--set speaker.frr.enabled=false \\\n"+
			"--namespace metallb-system --create-namespace --atomic --wait",
			b.SandboxBinDir, b.MetallbVersion),
		"sleep 60",
	}, "")
}

func (b *bashScriptStep) ConfigureMetalLB() automa.Builder {
	configScript := fmt.Sprintf(
		`set -eo pipefail; cat <<EOF | %s/kubectl apply -f - 
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
	return automa_steps.BashScriptStep(DeployMetallbConfigStepId, []string{
		configScript,
	}, "")
}

// CheckClusterHealth performs a series of checks to ensure the Kubernetes cluster is healthy
// This is a basic smoke tests to verify the cluster setup and can be extended as needed
func (b *bashScriptStep) CheckClusterHealth() automa.Builder {
	script := `
set -e

# Check if kubectl can access the cluster
kubectl get nodes

# Check all nodes are Ready
kubectl get nodes --no-headers | awk '{print $2}' | grep -v Ready && exit 1

# List of required namespaces
namespaces="cilium-secrets default kube-node-lease kube-public kube-system metallb-system"

for ns in $namespaces; do
  status=$(kubectl get namespace $ns --no-headers 2>/dev/null | awk '{print $2}')
  if [ "$status" != "Active" ]; then
    echo "Namespace $ns is not Active or does not exist (status: $status)"
    exit 1
  fi
done

# List of pod name prefixes to check in kube-system
prefixes="cilium- cilium-operator- coredns- etcd- hubble-relay- kube-apiserver- kube-controller-manager- kube-scheduler- metallb-controller- metallb-speaker-"

for prefix in $prefixes; do
  kubectl get pods -n kube-system --no-headers | awk -v p="$prefix" '$1 ~ "^"p {print $1, $2}' | while read name ready; do
    if ! [[ "$ready" =~ ^([0-9]+)/\1$ ]]; then
      echo "Pod $name is not fully ready ($ready)"
      exit 1
    fi
  done
done

# List of required services in the format namespace:service
services="default:kubernetes kube-system:hubble-peer kube-system:hubble-relay kube-system:kube-dns metallb-system:metallb-webhook-service"

for svc in $services; do
  ns=$(echo $svc | cut -d: -f1)
  name=$(echo $svc | cut -d: -f2)
  if ! kubectl get svc -n "$ns" "$name" --no-headers 2>/dev/null | grep -q .; then
    echo "Service $name not found in namespace $ns"
    exit 1
  fi
done

# List of required CRDs
crds="bfdprofiles.metallb.io bgpadvertisements.metallb.io bgppeers.metallb.io ciliumcidrgroups.cilium.io ciliumclusterwidenetworkpolicies.cilium.io ciliumendpoints.cilium.io ciliumidentities.cilium.io ciliuml2announcementpolicies.cilium.io ciliumloadbalancerippools.cilium.io ciliumnetworkpolicies.cilium.io ciliumnodeconfigs.cilium.io ciliumnodes.cilium.io ciliumpodippools.cilium.io communities.metallb.io ipaddresspools.metallb.io l2advertisements.metallb.io servicebgpstatuses.metallb.io servicel2statuses.metallb.io"

for crd in $crds; do
  if ! kubectl get crd "$crd" --no-headers 2>/dev/null | grep -q .; then
    echo "CRD $crd not found"
    exit 1
  fi
done

echo "Cluster health check passed"
`
	return automa_steps.BashScriptStep("check-cluster-health", []string{
		script,
	}, "")
}

// BashScriptBasedClusterSetupWorkflow returns a workflow that sets up a Kubernetes cluster using bash scripts
func BashScriptBasedClusterSetupWorkflow() automa.Builder {
	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes-cluster").
		Steps(
			// setup steps
			bashSteps.SetupSandboxFolders(),
			bashSteps.UpdateOS(),
			bashSteps.InstallGnupg2(),
			bashSteps.InstallConntrack(),
			bashSteps.InstallSoCat(),
			bashSteps.InstallEBTables(),
			bashSteps.InstallNFTables(),
			bashSteps.EnableAndStartNFTables(),
			bashSteps.AutoRemovePackages(),

			// setup env for k8s
			bashSteps.DisableSwap(),
			bashSteps.InstallKernelModules(),
			bashSteps.ConfigureSysctlForKubernetes(),
			bashSteps.SetupBindMounts(),

			// Install CLI tools
			bashSteps.DownloadKubectl(),
			bashSteps.InstallKubectl(),
			bashSteps.DownloadK9s(),
			bashSteps.InstallK9s(),
			bashSteps.DownloadHelm(),
			bashSteps.InstallHelm(), // requires helm to install metallb

			// In our Go based implementation we don't need this
			bashSteps.DownloadDasel(),
			bashSteps.InstallDasel(),

			// CRI-O setup
			bashSteps.DownloadCrio(),
			bashSteps.InstallCrio(),
			bashSteps.ConfigureSandboxCrio(),
			bashSteps.EnableAndStartCrio(),

			// kubeadm setup
			bashSteps.DownloadKubeadm(),
			bashSteps.DownloadKubeadmConfig(),
			bashSteps.InstallKubeadm(),
			bashSteps.TorchPriorKubeAdmConfiguration(),
			bashSteps.ConfigureKubeadm(),

			// kubelet setup
			// must be done after kubeadm config as kubelet.service.d requires the 10-kubeadm.conf file
			bashSteps.DownloadKubelet(),
			bashSteps.InstallKubelet(),
			bashSteps.DownloadKubeletConfig(),
			bashSteps.ConfigureKubelet(),
			bashSteps.EnableAndStartKubelet(),

			// init cluster
			bashSteps.InitCluster(),
			bashSteps.ConfigureKubeConfig(),

			// Cilium CNI setup
			bashSteps.DownloadCiliumCli(),
			bashSteps.InstallCiliumCli(),
			bashSteps.ConfigureCiliumCNI(),
			bashSteps.InstallCiliumCNI(),
			bashSteps.EnableAndStartCiliumCNI(),

			// MetalLB setup
			bashSteps.InstallMetalLB(),
			bashSteps.ConfigureMetalLB(),

			// Final health check
			bashSteps.CheckClusterHealth(),
		)
}
