package core

import (
	"path"
)

const (
	DefaultFilePerm = 0755
)

var (
	ProvisionerTempDir = "/tmp/provisioner"
	ProvisionerHomeDir = "/opt/solo/provisioner"
	BackupDir          = path.Join(ProvisionerHomeDir, "backup")
	SandboxDir         = path.Join(ProvisionerHomeDir, "sandbox")
	SandboxBinDir      = path.Join(SandboxDir, "bin")
	SandboxLocalBinDir = path.Join(SandboxDir, "usr", "local", "bin")
	DiagnosticsDir     = path.Join(ProvisionerTempDir, "diagnostics")
	KubernetesDir      = "/etc/kubernetes"
	KubeletDir         = "/var/lib/kubelet"
)
