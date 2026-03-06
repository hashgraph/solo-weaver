// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

const KubeletBinaryName = "kubelet"
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

	ki := &kubeletInstaller{
		baseInstaller: bi,
	}

	// Register the override so VerifyInstallation() calls kubelet-specific config checks
	ki.baseInstaller.verifyConfigured = ki.verifySandboxConfigs

	return ki, nil
}

// Install installs the kubelet binary and configuration files in the sandbox folder
func (ki *kubeletInstaller) Install() error {
	// Validate critical paths before proceeding
	if err := ki.validateCriticalPaths(); err != nil {
		return errorx.IllegalState.Wrap(err, "kubelet installation failed due to invalid paths")
	}

	// Install the kubelet binary using the common logic
	err := ki.baseInstaller.performInstall()
	if err != nil {
		return err
	}

	// Install the kubelet configuration files
	configDir := path.Join(models.Paths().SandboxDir, models.SystemdUnitFilesDir)
	err = ki.installConfig(configDir)
	if err != nil {
		return err
	}

	// Record installed state
	_ = ki.recordInstalled()

	return nil
}

// Uninstall removes the kubelet binary and configuration files from the sandbox folder
func (ki *kubeletInstaller) Uninstall() error {
	// Uninstall the kubelet binary using the common logic
	err := ki.baseInstaller.performUninstall()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubelet binary")
	}

	// Remove the kubelet configuration file
	configDir := path.Join(models.Paths().SandboxDir, models.SystemdUnitFilesDir)
	err = ki.baseInstaller.uninstallConfig(configDir)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubelet configuration files from %s", configDir)
	}

	// Remove recorded installed state
	_ = ki.clearInstalled()

	return nil
}

// Configure configures the kubelet binary,
// creates a copy of kubelet.service with updated paths in the sandbox folder,
// and creates symlink in systemd unit directory
func (ki *kubeletInstaller) Configure() error {
	// Create the symlink for the kubelet binary
	err := ki.baseInstaller.performConfiguration()
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

	// Record configured state
	_ = ki.recordConfigured()

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
	err = ki.baseInstaller.performConfigurationRemoval()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore kubelet binary configuration")
	}

	// Remove recorded configured state
	_ = ki.clearConfigured()

	return nil
}

// getKubeletServicePath returns the path to the kubelet.service file in the sandbox
func (ki *kubeletInstaller) getKubeletServicePath() string {
	return path.Join(models.Paths().SandboxDir, models.SystemdUnitFilesDir, kubeletServiceFileName)
}

// getSystemdUnitPath returns the path to the kubelet.service file in the systemd directory
func (ki *kubeletInstaller) getSystemdUnitPath() string {
	return path.Join(models.SystemdUnitFilesDir, kubeletServiceFileName)
}

// getSandboxKubeletBinPath returns the path to the kubelet binary in the sandbox
func (ki *kubeletInstaller) getSandboxKubeletBinPath() string {
	return path.Join(models.Paths().SandboxBinDir, "kubelet")
}

// validateCriticalPaths performs basic validation on critical paths used by the installer
func (ki *kubeletInstaller) validateCriticalPaths() error {
	paths := []string{
		models.Paths().SandboxDir,
		models.Paths().SandboxBinDir,
		models.SystemdUnitFilesDir,
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

// verifySandboxConfigs verifies that kubelet config files exist, are correctly patched,
// and that the systemd symlink is in place.
func (ki *kubeletInstaller) verifySandboxConfigs() (automa.StateBag, error) {
	meta := &automa.SyncStateBag{}

	// 1. Verify the kubelet.service file exists in the sandbox
	kubeletServicePath := ki.getKubeletServicePath()
	_, exists, err := ki.fileManager.PathExists(kubeletServicePath)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to check kubelet.service existence at %s", kubeletServicePath)
	}
	if !exists {
		return nil, errorx.IllegalState.New("kubelet.service not found at %s", kubeletServicePath)
	}

	meta.Set("kubeletServicePath", kubeletServicePath)

	// 2. Verify the service file is correctly patched
	content, err := ki.fileManager.ReadFile(kubeletServicePath, -1)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to read kubelet.service at %s", kubeletServicePath)
	}
	serviceStr := string(content)

	// Check the unpatched path is no longer present
	if strings.Contains(serviceStr, "/usr/bin/kubelet") {
		return nil, errorx.IllegalState.New("kubelet.service still contains unpatched /usr/bin/kubelet path")
	}

	// Check the sandbox path is present
	expectedKubeletBinPath := ki.getSandboxKubeletBinPath()
	if !strings.Contains(serviceStr, expectedKubeletBinPath) {
		return nil, errorx.IllegalState.New("kubelet.service does not contain expected sandbox kubelet path: %s", expectedKubeletBinPath)
	}
	meta.Set("sandboxKubeletPath", expectedKubeletBinPath)

	// 3. Verify the systemd symlink exists and points to the sandbox service file
	systemdUnitPath := ki.getSystemdUnitPath()
	_, exists, err = ki.fileManager.PathExists(systemdUnitPath)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to check systemd unit symlink at %s", systemdUnitPath)
	}
	if !exists {
		return nil, errorx.IllegalState.New("systemd unit symlink not found at %s", systemdUnitPath)
	}
	meta.Set("systemdUnitPath", systemdUnitPath)

	linkTarget, err := os.Readlink(systemdUnitPath)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to read systemd unit symlink at %s", systemdUnitPath)
	}
	if linkTarget != kubeletServicePath {
		return nil, errorx.IllegalState.New(
			"systemd unit symlink %s points to %s, expected %s",
			systemdUnitPath, linkTarget, kubeletServicePath,
		)
	}
	meta.Set("systemdUnitPathTarget", linkTarget)

	return meta, nil
}
