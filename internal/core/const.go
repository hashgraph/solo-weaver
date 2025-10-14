package core

import (
	"path"

	"golang.hedera.com/solo-provisioner/pkg/security"
)

const (
	DefaultFilePerm         = 0755
	DefaultProvisionerHome  = "/opt/provisioner"
	DefaultUnpackFolderName = "unpack"
	SystemBinDir            = "/usr/local/bin"
	SystemdUnitFilesDir     = "/usr/lib/systemd/system"
)

var (
	pp     = NewProvisionerPaths(DefaultProvisionerHome)
	svcAcc = security.ServiceAccount{
		UserName:  "provisioner",
		UserId:    "1000",
		GroupName: "provisioner",
		GroupId:   "1000",
	}
)

func init() {
	security.SetServiceAccount(svcAcc)
}

type ProvisionerPaths struct {
	HomeDir        string
	BinDir         string
	LogsDir        string
	UtilsDir       string
	DataDir        string
	ConfigDir      string
	BackupDir      string
	TempDir        string
	DiagnosticsDir string

	AllDirectories []string

	// Sandbox directories for isolated binaries

	SandboxDir         string
	SandboxBinDir      string
	SandboxLocalBinDir string
	SandboxDirectories []string // all sandbox related directories

	// Kubernetes related directories
	KubeletDir string
	KubectlDir string
	KubeadmDir string
	HelmDir    string
	K9sDir     string
	CrioDir    string
	CiliumDir  string
}

func NewProvisionerPaths(home string) *ProvisionerPaths {
	pp := &ProvisionerPaths{
		HomeDir:        home,
		BinDir:         path.Join(home, "bin"),
		LogsDir:        path.Join(home, "logs"),
		DataDir:        path.Join(home, "data"),
		ConfigDir:      path.Join(home, "config"),
		UtilsDir:       path.Join(home, "utils"),
		BackupDir:      path.Join(home, "backup"),
		TempDir:        path.Join(home, "tmp"),
		DiagnosticsDir: path.Join(home, "tmp", "diagnostics"),

		KubeletDir: path.Join(home, "kubelet"),
		KubectlDir: path.Join(home, "kubectl"),
		KubeadmDir: path.Join(home, "kubeadm"),
		HelmDir:    path.Join(home, "helm"),
		K9sDir:     path.Join(home, "k9s"),
		CrioDir:    path.Join(home, "crio"),
		CiliumDir:  path.Join(home, "cilium"),
	}

	pp.SandboxDir = path.Join(pp.HomeDir, "sandbox")
	pp.SandboxBinDir = path.Join(pp.SandboxDir, "bin")
	pp.SandboxLocalBinDir = path.Join(pp.SandboxDir, "usr", "local", "bin")

	pp.SandboxDirectories = []string{
		pp.SandboxDir,
		pp.SandboxBinDir,
		pp.SandboxLocalBinDir,
		path.Join(pp.SandboxDir, "etc/crio/keys"),
		path.Join(pp.SandboxDir, "etc/default"),
		path.Join(pp.SandboxDir, "etc/sysconfig"),
		path.Join(pp.SandboxDir, "etc/provisioner"),
		path.Join(pp.SandboxDir, "etc/containers/registries.conf.d"),
		path.Join(pp.SandboxDir, "etc/cni/net.d"),
		path.Join(pp.SandboxDir, "etc/nri/conf.d"),
		path.Join(pp.SandboxDir, "etc/kubernetes/pki"),
		path.Join(pp.SandboxDir, "var/lib/etcd"),
		path.Join(pp.SandboxDir, "var/lib/containers/storage"),
		path.Join(pp.SandboxDir, "var/lib/kubelet"),
		path.Join(pp.SandboxDir, "var/lib/crio"),
		path.Join(pp.SandboxDir, "var/run/cilium"),
		path.Join(pp.SandboxDir, "var/run/nri"),
		path.Join(pp.SandboxDir, "var/run/containers/storage"),
		path.Join(pp.SandboxDir, "var/run/crio/exits"),
		path.Join(pp.SandboxDir, "var/logs/crio/pods"),
		path.Join(pp.SandboxDir, "run/runc"),
		path.Join(pp.SandboxDir, "usr/libexec/crio"),
		path.Join(pp.SandboxDir, "usr/lib/systemd/system/kubelet.service.d"),
		path.Join(pp.SandboxDir, "usr/local/share/man"),
		path.Join(pp.SandboxDir, "usr/local/share/oci-umount/oci-umount.d"),
		path.Join(pp.SandboxDir, "usr/local/share/bash-completion/completions"),
		path.Join(pp.SandboxDir, "usr/local/share/fish/completions"),
		path.Join(pp.SandboxDir, "usr/local/share/zsh/site-functions"),
		path.Join(pp.SandboxDir, "opt/cni/bin"),
		path.Join(pp.SandboxDir, "opt/nri/plugins"),
	}

	// populate AllDirectories
	pp.AllDirectories = []string{
		pp.HomeDir,
		pp.BinDir,
		pp.LogsDir,
		pp.DataDir,
		pp.UtilsDir,
		pp.ConfigDir,
		pp.BackupDir,
		pp.TempDir,
		pp.DiagnosticsDir,
		pp.KubeletDir,
		pp.KubectlDir,
		pp.KubeadmDir,
		pp.K9sDir,
		pp.HelmDir,
		pp.CrioDir,
		path.Join(pp.CrioDir, "unpack"),
		pp.CiliumDir,
	}
	pp.AllDirectories = append(pp.AllDirectories, pp.SandboxDirectories...)

	return pp
}

func Paths() *ProvisionerPaths {
	return pp
}

func ServiceAccount() security.ServiceAccount {
	return svcAcc
}
