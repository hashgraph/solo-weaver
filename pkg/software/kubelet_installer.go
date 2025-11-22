package software

import (
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
)

const KubeletServiceName = "kubelet"
const kubeletServiceFileName = "kubelet.service"

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
	// Validate critical paths before proceeding
	if err := ki.validateCriticalPaths(); err != nil {
		return errorx.IllegalState.Wrap(err, "kubelet installation failed due to invalid paths")
	}

	// Install the kubelet binary using the common logic
	err := ki.baseInstaller.Install()
	if err != nil {
		return err
	}

	// Install the kubelet configuration files
	configDir := path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir)
	err = ki.installConfig(configDir)
	if err != nil {
		return err
	}

	return nil
}

// Uninstall removes the kubelet binary and configuration files from the sandbox folder
func (ki *kubeletInstaller) Uninstall() error {
	// Uninstall the kubelet binary using the common logic
	err := ki.baseInstaller.Uninstall()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubelet binary")
	}

	// Remove the kubelet configuration file
	configDir := path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir)
	err = ki.baseInstaller.uninstallConfig(configDir)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubelet configuration files from %s", configDir)
	}

	return nil
}

// Configure configures the kubelet binary,
// creates a copy of kubelet.service with updated paths in the sandbox folder,
// and creates symlink in systemd unit directory
func (ki *kubeletInstaller) Configure() error {
	// Create the symlink for the kubelet binary
	err := ki.baseInstaller.Configure()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to configure kubelet binary")
	}

	// Create the latest service file with updated paths
	err = ki.patchServiceFile()
	if err != nil {
		return err
	}

	// Create symlink in systemd directory
	err = ki.createSystemdSymlink()
	if err != nil {
		return err
	}

	return nil
}

// RestoreConfiguration restores the kubelet binary and configuration files to their original state
func (ki *kubeletInstaller) RemoveConfiguration() error {
	// Remove the symlink for kubelet.service config file
	systemdUnitPath := ki.getSystemdUnitPath()
	err := ki.fileManager.RemoveAll(systemdUnitPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove symlink for kubelet service file at %s", systemdUnitPath)
	}

	// Call base implementation to cleanup symlinks
	err = ki.baseInstaller.RemoveConfiguration()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore kubelet binary configuration")
	}

	return nil
}

// getKubeletServicePath returns the path to the kubelet.service file in the sandbox
func (ki *kubeletInstaller) getKubeletServicePath() string {
	return path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir, kubeletServiceFileName)
}

// getSystemdUnitPath returns the path to the kubelet.service file in the systemd directory
func (ki *kubeletInstaller) getSystemdUnitPath() string {
	return path.Join(core.SystemdUnitFilesDir, kubeletServiceFileName)
}

// getSandboxKubeletBinPath returns the path to the kubelet binary in the sandbox
func (ki *kubeletInstaller) getSandboxKubeletBinPath() string {
	return path.Join(core.Paths().SandboxBinDir, "kubelet")
}

// validateCriticalPaths performs basic validation on critical paths used by the installer
func (ki *kubeletInstaller) validateCriticalPaths() error {
	paths := []string{
		core.Paths().SandboxDir,
		core.Paths().SandboxBinDir,
		core.SystemdUnitFilesDir,
	}

	for _, p := range paths {
		if p == "" {
			return errorx.IllegalArgument.New("critical path cannot be empty: %s", p)
		}
		if !path.IsAbs(p) {
			return errorx.IllegalArgument.New("critical path must be absolute: %s", p)
		}
	}
	return nil
}

// patchServiceFile patches kubelet.service with updated paths in place
func (ki *kubeletInstaller) patchServiceFile() error {
	kubeletServicePath := ki.getKubeletServicePath()

	// Replace the kubelet binary path with sandbox path
	sandboxKubeletPath := ki.getSandboxKubeletBinPath()
	err := ki.replaceAllInFile(kubeletServicePath, "/usr/bin/kubelet", sandboxKubeletPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in kubelet.service file")
	}

	return nil
}

// createSystemdSymlink creates a symlink for the kubelet service in the systemd directory
func (ki *kubeletInstaller) createSystemdSymlink() error {
	kubeletServicePath := ki.getKubeletServicePath()
	systemdUnitPath := ki.getSystemdUnitPath()

	err := ki.fileManager.CreateSymbolicLink(kubeletServicePath, systemdUnitPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for kubelet service from %s to %s", kubeletServicePath, systemdUnitPath)
	}

	return nil
}
