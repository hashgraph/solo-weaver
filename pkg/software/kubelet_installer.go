// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

const KubeletBinaryName = "kubelet"
const KubeletServiceName = "kubelet"
const kubeletServiceFileName = "kubelet.service"
const kubeletServiceDropInDirName = "kubelet.service.d"
const kubeletCrioOrderingFileName = "10-crio-ordering.conf"

// kubeletCrioOrderingDropIn orders kubelet after cri-o so cAdvisor (vendored in
// kubelet) can reach cri-o's API socket — and its /var/run/crio/crio.sock bridge —
// at startup to register the "crio-images" imagefs label. Without this ordering,
// kubelet's eviction manager logs `non-existent label "crio-images"` on every sync.
// See https://github.com/hashgraph/solo-weaver/issues/22.
const kubeletCrioOrderingDropIn = `# Managed by solo-weaver. cAdvisor (vendored in kubelet) builds its filesystem
# label table once at startup and must reach cri-o to register "crio-images".
# Order kubelet after cri-o so the socket and its bridge symlink are live first.
# See https://github.com/hashgraph/solo-weaver/issues/22.
[Unit]
Wants=crio.service
After=crio.service
`

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
	return ki.recordInstalled()
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

	// Remove the sandbox crio-ordering drop-in file (issue #22)
	sandboxDropIn := ki.getKubeletCrioOrderingSandboxPath()
	err = ki.fileManager.RemoveAll(sandboxDropIn)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove kubelet crio-ordering drop-in at %s", sandboxDropIn)
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

	// Order kubelet after cri-o so cAdvisor can reach the cri-o socket at startup (issue #22)
	err = ki.createCrioOrderingDropIn()
	if err != nil {
		return err
	}

	// Record configured state
	return ki.recordConfigured()
}

// RestoreConfiguration restores the kubelet binary and configuration files to their original state
func (ki *kubeletInstaller) RemoveConfiguration() error {
	// Remove the symlink for kubelet.service config file
	systemdUnitPath := ki.getSystemdUnitPath()
	err := ki.fileManager.RemoveAll(systemdUnitPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove symlink for kubelet service file at %s", systemdUnitPath)
	}

	// Remove only our crio-ordering drop-in symlink; the shared kubelet.service.d
	// directory (also holding kubeadm's 10-kubeadm.conf) is left intact (issue #22)
	crioOrderingPath := ki.getKubeletCrioOrderingSystemPath()
	err = ki.fileManager.RemoveAll(crioOrderingPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove kubelet crio-ordering drop-in symlink at %s", crioOrderingPath)
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

// getKubeletDropInDir returns the sandbox kubelet.service.d drop-in directory.
func (ki *kubeletInstaller) getKubeletDropInDir() string {
	return path.Join(models.Paths().SandboxDir, models.SystemdUnitFilesDir, kubeletServiceDropInDirName)
}

// getKubeletCrioOrderingSandboxPath returns the sandbox path to the crio-ordering drop-in.
func (ki *kubeletInstaller) getKubeletCrioOrderingSandboxPath() string {
	return path.Join(ki.getKubeletDropInDir(), kubeletCrioOrderingFileName)
}

// getKubeletCrioOrderingSystemPath returns the host path to the crio-ordering drop-in symlink.
func (ki *kubeletInstaller) getKubeletCrioOrderingSystemPath() string {
	return path.Join(models.SystemdUnitFilesDir, kubeletServiceDropInDirName, kubeletCrioOrderingFileName)
}

// createCrioOrderingDropIn writes the kubelet drop-in that orders kubelet after cri-o
// and symlinks it into the host kubelet.service.d directory. The host directory is
// shared with kubeadm's 10-kubeadm.conf, so only the individual .conf file is symlinked.
// See issue #22.
func (ki *kubeletInstaller) createCrioOrderingDropIn() error {
	// Ensure the sandbox drop-in directory exists
	sandboxDir := ki.getKubeletDropInDir()
	if err := ki.fileManager.CreateDirectory(sandboxDir, true); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create kubelet.service.d directory at %s", sandboxDir)
	}

	// Write the drop-in file in the sandbox
	sandboxPath := ki.getKubeletCrioOrderingSandboxPath()
	if err := os.WriteFile(sandboxPath, []byte(kubeletCrioOrderingDropIn), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write kubelet crio-ordering drop-in at %s", sandboxPath)
	}

	// Create the host drop-in directory (shared with kubeadm) and symlink the file into it
	systemDir := path.Join(models.SystemdUnitFilesDir, kubeletServiceDropInDirName)
	if err := ki.fileManager.CreateDirectory(systemDir, true); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create host kubelet.service.d directory at %s", systemDir)
	}
	systemPath := ki.getKubeletCrioOrderingSystemPath()
	if err := ki.fileManager.CreateSymbolicLink(sandboxPath, systemPath, true); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create kubelet crio-ordering drop-in symlink from %s to %s", sandboxPath, systemPath)
	}

	return nil
}

// verifySandboxConfigs verifies that kubelet config files exist, are correctly patched,
// and that the systemd symlink is in place.
func (ki *kubeletInstaller) verifySandboxConfigs() (models.StringMap, error) {
	meta := models.NewStringMap()

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

	// 4. Verify the crio-ordering drop-in exists in the sandbox and is symlinked into the host (issue #22)
	sandboxDropIn := ki.getKubeletCrioOrderingSandboxPath()
	_, exists, err = ki.fileManager.PathExists(sandboxDropIn)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to check kubelet crio-ordering drop-in at %s", sandboxDropIn)
	}
	if !exists {
		return nil, errorx.IllegalState.New("kubelet crio-ordering drop-in not found at %s", sandboxDropIn)
	}
	meta.Set("kubeletCrioOrderingSandboxPath", sandboxDropIn)

	systemDropIn := ki.getKubeletCrioOrderingSystemPath()
	_, exists, err = ki.fileManager.PathExists(systemDropIn)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to check kubelet crio-ordering drop-in symlink at %s", systemDropIn)
	}
	if !exists {
		return nil, errorx.IllegalState.New("kubelet crio-ordering drop-in symlink not found at %s", systemDropIn)
	}
	dropInTarget, err := os.Readlink(systemDropIn)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to read kubelet crio-ordering drop-in symlink at %s", systemDropIn)
	}
	if dropInTarget != sandboxDropIn {
		return nil, errorx.IllegalState.New(
			"kubelet crio-ordering drop-in symlink %s points to %s, expected %s",
			systemDropIn, dropInTarget, sandboxDropIn,
		)
	}
	meta.Set("kubeletCrioOrderingSystemPath", systemDropIn)

	return meta, nil
}
