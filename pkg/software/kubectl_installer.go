package software

import (
	"path"

	"golang.hedera.com/solo-provisioner/internal/core"
)

type kubectlInstaller struct {
	*BaseInstaller
}

var _ Software = (*kubectlInstaller)(nil)

func NewKubectlInstaller() (Software, error) {
	baseInstaller, err := NewBaseInstaller("kubectl", "")
	if err != nil {
		return nil, err
	}

	return &kubectlInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

func (ki *kubectlInstaller) Download() error {
	return ki.BaseInstaller.Download()
}

func (ki *kubectlInstaller) Extract() error {
	// kubectl is typically a single binary, no extraction needed
	return nil
}

func (ki *kubectlInstaller) Install() error {
	return ki.BaseInstaller.Install()
}

func (ki *kubectlInstaller) Verify() error {
	return ki.BaseInstaller.Verify()
}

func (ki *kubectlInstaller) IsInstalled() (bool, error) {
	return ki.BaseInstaller.IsInstalled()
}

func (ki *kubectlInstaller) Configure() error {
	fileManager := ki.FileManager()
	sandboxBinary := path.Join(core.Paths().SandboxBinDir, "kubectl")

	// Create symlink to /usr/local/bin for system-wide access
	systemBinary := "/usr/local/bin/kubectl"

	// Create new symlink
	err := fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
	if err != nil {
		return NewInstallationError(err, sandboxBinary, systemBinary)
	}

	return nil
}

func (ki *kubectlInstaller) IsConfigured() (bool, error) {
	return false, nil
}
