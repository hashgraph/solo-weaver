package software

import (
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
)

const (
	kubeletServiceDir = "/usr/lib/systemd/system/kubelet.service.d"
)

type kubeadmInstaller struct {
	*BaseInstaller
}

var _ Software = (*kubeadmInstaller)(nil)

func NewKubeadmInstaller() (Software, error) {
	baseInstaller, err := NewBaseInstaller("kubeadm", "")
	if err != nil {
		return nil, err
	}

	return &kubeadmInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

// Download downloads the kubeadm binary and configuration files
func (ki *kubeadmInstaller) Download() error {
	// Download kubeadm binary using base implementation
	if err := ki.BaseInstaller.Download(); err != nil {
		return err
	}

	// Download kubeadm configuration files (specific to kubeadm)
	if err := ki.BaseInstaller.DownloadConfigFiles(); err != nil {
		return err
	}

	return nil
}

func (ki *kubeadmInstaller) Extract() error {
	// Kubeadm might not need extraction
	return nil
}

func (ki *kubeadmInstaller) Install() error {
	// Install the kubeadm binary using the common logic
	err := ki.BaseInstaller.Install()
	if err != nil {
		return err
	}

	// Install the kubeadm configuration files
	err = ki.BaseInstaller.InstallConfigFiles(path.Join(core.Paths().SandboxDir, kubeletServiceDir))
	if err != nil {
		return err
	}

	return nil
}

func (ki *kubeadmInstaller) Verify() error {
	return ki.BaseInstaller.Verify()
}

func (ki *kubeadmInstaller) IsInstalled() (bool, error) {
	return ki.BaseInstaller.IsInstalled()
}

func (ki *kubeadmInstaller) Configure() error {
	fileManager := ki.FileManager()
	sandboxBinary := path.Join(core.Paths().SandboxBinDir, "kubeadm")

	// Create symlink to /usr/local/bin for system-wide access
	systemBinary := "/usr/local/bin/kubeadm"

	// Create new symlink
	err := fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
	if err != nil {
		return NewInstallationError(err, sandboxBinary, systemBinary)
	}

	// Replace strings in configuration file and create symlink

	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDir)
	kubeadmConfDest := path.Join(sandboxKubeletServiceDir, "10-kubeadm.conf")

	err = ki.replaceAllInFile(kubeadmConfDest, "/usr/bin/kubelet", path.Join(core.Paths().SandboxBinDir, "kubelet"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in 10-kubeadm.conf")
	}

	err = fileManager.CreateSymbolicLink(sandboxKubeletServiceDir, kubeletServiceDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for kubelet service directory")
	}

	return nil
}

func (ki *kubeadmInstaller) IsConfigured() (bool, error) {
	return false, nil
}
