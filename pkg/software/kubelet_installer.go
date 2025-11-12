package software

import (
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
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
	err := ki.baseInstaller.RemoveConfiguration()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore kubelet binary configuration")
	}

	// Remove the latest file
	latestServicePath := ki.getLatestKubeletServicePath()
	err = ki.fileManager.RemoveAll(latestServicePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove latest kubelet.service file at %s", latestServicePath)
	}

	// Remove the symlink for kubelet.service config file
	systemdUnitPath := ki.getSystemdUnitPath()
	err = ki.fileManager.RemoveAll(systemdUnitPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove symlink for kubelet service file at %s", systemdUnitPath)
	}

	return nil
}

// Overrides IsInstalled() to check if both binaries and config files are installed
func (ki *kubeletInstaller) IsInstalled() (bool, error) {
	binariesInstalled, err := ki.baseInstaller.IsInstalled()
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubelet binaries are installed")
	}

	configDir := path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir)
	configsInstalled, err := ki.isConfigInstalled(configDir)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubelet configuration files are installed in %s", configDir)
	}

	return binariesInstalled && configsInstalled, nil
}

// Overrides IsConfigured() to check if the kubelet is properly configured.
// This includes checking if the binaries are configured, the .latest service file is valid,
// and the systemd symlink for kubelet.service is present.
func (ki *kubeletInstaller) IsConfigured() (bool, error) {
	// First check if binaries are configured
	binariesConfigured, err := ki.baseInstaller.IsConfigured()
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubelet binaries are configured")
	}
	if !binariesConfigured {
		return false, nil
	}

	// Check if the .latest service file is valid
	latestFileValid, err := ki.isLatestServiceFileValid()
	if err != nil {
		return false, err
	}
	if !latestFileValid {
		return false, nil
	}

	// Check if the systemd symlink is present
	symlinkPresent, err := ki.isSystemdSymlinkPresent()
	if err != nil {
		return false, err
	}

	return symlinkPresent, nil
}

// getKubeletServicePath returns the path to the kubelet.service file in the sandbox
func (ki *kubeletInstaller) getKubeletServicePath() string {
	return path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir, kubeletServiceFileName)
}

// getLatestKubeletServicePath returns the path to the .latest kubelet.service file in the sandbox
func (ki *kubeletInstaller) getLatestKubeletServicePath() string {
	return ki.getKubeletServicePath() + ".latest"
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

// patchServiceFile creates a copy of kubelet.service with updated paths
func (ki *kubeletInstaller) patchServiceFile() error {
	kubeletServicePath := ki.getKubeletServicePath()
	latestServicePath := ki.getLatestKubeletServicePath()

	// Create latest file which will have some strings replaced
	err := ki.fileManager.CopyFile(kubeletServicePath, latestServicePath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create latest kubelet.service file at %s", latestServicePath)
	}

	// Replace the kubelet binary path with sandbox path
	sandboxKubeletPath := ki.getSandboxKubeletBinPath()
	err = ki.replaceAllInFile(latestServicePath, "/usr/bin/kubelet", sandboxKubeletPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in kubelet.service file")
	}

	return nil
}

// createSystemdSymlink creates a symlink for the kubelet service in the systemd directory
func (ki *kubeletInstaller) createSystemdSymlink() error {
	latestServicePath := ki.getLatestKubeletServicePath()
	systemdUnitPath := ki.getSystemdUnitPath()

	err := ki.fileManager.CreateSymbolicLink(latestServicePath, systemdUnitPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for kubelet service from %s to %s", latestServicePath, systemdUnitPath)
	}

	return nil
}

// isLatestServiceFileValid checks if the .latest service file exists and has the correct content
func (ki *kubeletInstaller) isLatestServiceFileValid() (bool, error) {
	latestServicePath := ki.getLatestKubeletServicePath()

	// Check if the .latest file exists
	fi, exists, err := ki.fileManager.PathExists(latestServicePath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if latest service file exists at %s", latestServicePath)
	}
	if !exists || !ki.fileManager.IsRegularFileByFileInfo(fi) {
		return false, nil
	}

	// Read the original service file to create expected content
	originalServicePath := ki.getKubeletServicePath()
	originalContent, err := ki.fileManager.ReadFile(originalServicePath, -1)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read original kubelet.service file at %s", originalServicePath)
	}

	// Generate expected content with replaced paths
	sandboxKubeletPath := ki.getSandboxKubeletBinPath()
	expectedContent := strings.ReplaceAll(string(originalContent), "/usr/bin/kubelet", sandboxKubeletPath)

	// Read the actual latest file content
	actualContent, err := ki.fileManager.ReadFile(latestServicePath, -1)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read latest kubelet.service file at %s", latestServicePath)
	}

	// Compare contents
	return string(actualContent) == expectedContent, nil
}

// isSystemdSymlinkPresent checks if the kubelet.service symlink exists in the systemd directory
func (ki *kubeletInstaller) isSystemdSymlinkPresent() (bool, error) {
	systemdUnitPath := ki.getSystemdUnitPath()

	_, exists, err := ki.fileManager.PathExists(systemdUnitPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if systemd symlink exists at %s", systemdUnitPath)
	}

	return exists, nil
}
