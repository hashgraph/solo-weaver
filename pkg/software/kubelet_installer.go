package software

import (
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
)

const KubeletServiceName = "kubelet"

type kubeletInstaller struct {
	*baseInstaller
}

func NewKubeletInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("kubelet", opts...)
	if err != nil {
		return nil, err
	}

	return &kubeletInstaller{
		baseInstaller: bi,
	}, nil
}

// Install installs the kubelet binary and configuration files in the sandbox folder
func (ki *kubeletInstaller) Install() error {
	// Install the kubeadm binary using the common logic
	err := ki.baseInstaller.Install()
	if err != nil {
		return err
	}

	// Install the kubelet configuration files
	err = ki.installConfig(path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir))
	if err != nil {
		return err
	}

	return nil
}

// Configure configures the kubelet binary,
// updates kubelet.service and create symlink in systemd unit directory
func (ki *kubeletInstaller) Configure() error {
	// Create the symlink for the kubelet binary
	err := ki.baseInstaller.Configure()
	if err != nil {
		return err
	}

	fileManager := ki.fileManager

	// Replace strings in configuration file and create symlink
	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir)
	kubeadmConfDest := path.Join(sandboxKubeletServiceDir, "kubelet.service")

	err = ki.replaceAllInFile(kubeadmConfDest, "/usr/bin/kubelet", path.Join(core.Paths().SandboxBinDir, "kubelet"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in kubelet.service")
	}

	err = fileManager.CreateSymbolicLink(kubeadmConfDest, path.Join(core.SystemdUnitFilesDir, "kubelet.service"), true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for kubelet service directory")
	}

	return nil
}
