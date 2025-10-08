package core

import (
	"path"
)

const (
	DefaultFilePerm        = 0755
	DefaultProvisionerHome = "/opt/provisioner"
	DefaultKubernetesDir   = "/etc/kubernetes"
	DefaultKubeletDir      = "/var/lib/kubelet"
)

var (
	pp = NewProvisionerPaths(DefaultProvisionerHome)
)

type ProvisionerPaths struct {
	HomeDir        string
	BinDir         string
	LogsDir        string
	DataDir        string
	ConfigDir      string
	CacheDir       string
	BackupDir      string
	TempDir        string
	DiagnosticsDir string

	AllDirectories []string

	// Sandbox directories for isolated binaries

	SandboxDir         string
	SandboxBinDir      string
	SandboxLocalBinDir string

	// Kubernetes related directories
	KubernetesDir string
	KubeletDir    string
}

func NewProvisionerPaths(home string) *ProvisionerPaths {
	pp := &ProvisionerPaths{
		HomeDir:        home,
		BinDir:         path.Join(home, "bin"),
		LogsDir:        path.Join(home, "logs"),
		DataDir:        path.Join(home, "data"),
		ConfigDir:      path.Join(home, "config"),
		CacheDir:       path.Join(home, "cache"),
		BackupDir:      path.Join(home, "backup"),
		TempDir:        path.Join(home, "tmp"),
		DiagnosticsDir: path.Join(home, "tmp", "diagnostics"),
		SandboxDir:     path.Join(home, "sandbox"),
	}

	pp.SandboxBinDir = path.Join(pp.SandboxDir, "bin")
	pp.SandboxLocalBinDir = path.Join(pp.SandboxDir, "usr", "local", "bin")
	pp.KubernetesDir = DefaultKubernetesDir
	pp.KubeletDir = DefaultKubeletDir

	// populate AllDirectories
	pp.AllDirectories = []string{
		pp.BinDir,
		pp.LogsDir,
		pp.DataDir,
		pp.ConfigDir,
		pp.CacheDir,
		pp.BackupDir,
		pp.TempDir,
		pp.DiagnosticsDir,
		pp.SandboxDir,
		pp.SandboxBinDir,
		pp.SandboxLocalBinDir,
	}

	return pp
}

func Paths() *ProvisionerPaths {
	return pp
}
