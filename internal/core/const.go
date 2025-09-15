package core

import "path"

const (
	DefaultFilePerm = 0755
)

var (
	ProvisionerTempDir = "/tmp/provisioner"
	ProvisionerHomeDir = "/opt/provisioner"
	SandboxHomeDir     = path.Join(ProvisionerHomeDir, "sandbox")
	SandboxBinDir      = path.Join(SandboxHomeDir, "bin")
	SandboxLocalBinDir = path.Join(SandboxHomeDir, "usr", "local", "bin")
	DiagnosticsDir     = path.Join(ProvisionerTempDir, "diagnostics")
)
