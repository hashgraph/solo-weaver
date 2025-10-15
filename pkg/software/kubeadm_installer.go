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
	*baseInstaller
}

func NewKubeadmInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("kubeadm", opts...)
	if err != nil {
		return nil, err
	}

	return &kubeadmInstaller{
		baseInstaller: bi,
	}, nil
}

// Install installs the kubeadm binary and configuration files in the sandbox folder
func (ki *kubeadmInstaller) Install() error {
	// Install the kubeadm binary using the common logic
	err := ki.baseInstaller.Install()
	if err != nil {
		return err
	}

	// Install the kubeadm configuration files
	err = ki.installConfig(path.Join(core.Paths().SandboxDir, kubeletServiceDir))
	if err != nil {
		return err
	}

	return nil
}

// Configure configures the kubeadm binary,
// updates 10-kubeadm.conf and create symlink for kubelet service directory
func (ki *kubeadmInstaller) Configure() error {
	// Create the symlink for the kubeadm binary
	err := ki.baseInstaller.Configure()
	if err != nil {
		return err
	}

	fileManager := ki.fileManager

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
