// SPDX-License-Identifier: Apache-2.0

package software

import (
	"context"
	"os"
	"os/exec"
	"path"
	"sort"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

const (
	TeleportBinaryName      = "teleport"
	TeleportServiceName     = "teleport"
	teleportServiceFileName = "teleport.service"
	teleportConfigDir       = "/etc/teleport"
	teleportConfigFile      = "/etc/teleport/teleport.yaml"
	// Path to the service file inside the archive
	teleportServiceArchivePath = "teleport-ent/examples/systemd/production/node/teleport.service"
)

// TeleportNodeAgentConfigureOptions holds options for configuring the Teleport node agent
type TeleportNodeAgentConfigureOptions struct {
	// ProxyAddr is the address of the Teleport proxy server (e.g., "hashgraph.teleport.sh:443")
	ProxyAddr string
	// JoinToken is the token used to join the Teleport cluster
	JoinToken string
}

type teleportNodeAgentInstaller struct {
	*baseInstaller
	configureOpts *TeleportNodeAgentConfigureOptions
}

// NewTeleportNodeAgentInstaller creates a new installer for the Teleport node agent
func NewTeleportNodeAgentInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("teleport", opts...)
	if err != nil {
		return nil, err
	}

	return &teleportNodeAgentInstaller{
		baseInstaller: bi,
	}, nil
}

// NewTeleportNodeAgentInstallerWithConfig creates a new Teleport node agent installer with configuration options
// This is used when setting up the node agent with proxy address and join token
func NewTeleportNodeAgentInstallerWithConfig(configOpts *TeleportNodeAgentConfigureOptions, opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("teleport", opts...)
	if err != nil {
		return nil, err
	}

	return &teleportNodeAgentInstaller{
		baseInstaller: bi,
		configureOpts: configOpts,
	}, nil
}

// Install installs the teleport binaries and service file in the sandbox folder
func (ti *teleportNodeAgentInstaller) Install() error {
	// Validate critical paths before proceeding
	if err := ti.validateCriticalPaths(); err != nil {
		return errorx.IllegalState.Wrap(err, "teleport installation failed due to invalid paths")
	}

	// Install the teleport binaries using the common logic
	err := ti.baseInstaller.performInstall()
	if err != nil {
		return err
	}

	// Install the service file from the archive to the sandbox
	err = ti.installServiceFile()
	if err != nil {
		return err
	}

	// Record installed state
	return ti.recordInstalled()
}

// Uninstall removes the teleport binaries from the sandbox folder
func (ti *teleportNodeAgentInstaller) Uninstall() error {
	// Uninstall the teleport binaries using the common logic
	err := ti.baseInstaller.performUninstall()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall teleport binaries")
	}

	// Remove recorded installed state
	_ = ti.clearInstalled()

	return nil
}

// Configure configures the teleport node agent by:
// 1. Creating symlinks for binaries
// 2. Running "teleport configure" to generate the configuration file
// 3. Patching the systemd service file with correct paths
// 4. Creating symlink for the service file in systemd directory
func (ti *teleportNodeAgentInstaller) Configure() error {
	if ti.configureOpts == nil {
		return errorx.IllegalArgument.New("teleport configure options not provided")
	}

	if ti.configureOpts.ProxyAddr == "" {
		return errorx.IllegalArgument.New("teleport proxy address is required")
	}

	if ti.configureOpts.JoinToken == "" {
		return errorx.IllegalArgument.New("teleport join token is required")
	}

	// Create the symlinks for the teleport binaries
	err := ti.baseInstaller.performConfiguration()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to configure teleport binaries")
	}

	// Create teleport config directory
	if err := os.MkdirAll(teleportConfigDir, 0755); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create teleport config directory")
	}

	// Run teleport configure to generate the configuration file
	err = ti.runTeleportConfigure()
	if err != nil {
		return err
	}

	// Patch the systemd service file with correct paths
	err = ti.patchServiceFile()
	if err != nil {
		return err
	}

	// Create symlink in systemd directory
	err = ti.createSystemdSymlink()
	if err != nil {
		return err
	}

	// Record configured state
	return ti.recordConfigured()
}

// RemoveConfiguration removes the teleport configuration and symlinks
func (ti *teleportNodeAgentInstaller) RemoveConfiguration() error {
	// Remove the symlink for teleport.service config file
	systemdUnitPath := ti.getSystemdUnitPath()
	err := ti.fileManager.RemoveAll(systemdUnitPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove symlink for teleport service file at %s", systemdUnitPath)
	}

	// Remove the teleport config file
	if err := ti.fileManager.RemoveAll(teleportConfigFile); err != nil {
		// Log but don't fail - config file might not exist
	}

	// Call base implementation to cleanup symlinks
	err = ti.baseInstaller.performConfigurationRemoval()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore teleport configuration")
	}

	// Remove recorded configured state
	_ = ti.clearConfigured()

	return nil
}

// runTeleportConfigure runs the teleport configure command to generate the configuration file
// See: https://goteleport.com/docs/reference/cli/teleport/#teleport-configure
func (ti *teleportNodeAgentInstaller) runTeleportConfigure() error {
	teleportBinary := ti.getSandboxTeleportBinPath()

	// Verify the teleport binary before executing it (guards against tampering).
	if err := verifyExecutables(TeleportServiceName); err != nil {
		return err
	}

	// Build the configure command arguments
	// Using teleport configure to set up the node agent
	configureArgs := []string{
		"configure",
		"--roles=node",
		"--proxy=" + ti.configureOpts.ProxyAddr,
		"--token=" + ti.configureOpts.JoinToken,
		"--output=" + teleportConfigFile,
	}

	ctx := context.Background()
	configureCmd := exec.CommandContext(ctx, teleportBinary, configureArgs...)
	configureCmd.Stdout = os.Stdout
	configureCmd.Stderr = os.Stderr

	if err := configureCmd.Run(); err != nil {
		return errorx.InternalError.Wrap(err, "failed to run teleport configure")
	}

	return nil
}

// installServiceFile copies the teleport.service file from the extracted archive to the sandbox
func (ti *teleportNodeAgentInstaller) installServiceFile() error {
	// Source path in the extract folder
	extractFolder := ti.baseInstaller.extractFolder()
	sourcePath := path.Join(extractFolder, teleportServiceArchivePath)

	// Verify that the service file exists in the extract folder
	_, exists, err := ti.fileManager.PathExists(sourcePath)
	if err != nil || !exists {
		return errorx.IllegalState.Wrap(err, "teleport.service file not found at %s", sourcePath)
	}

	// Destination path in the sandbox (rename to just teleport.service)
	sandboxServicePath := ti.getTeleportServicePath()

	// Ensure the destination directory exists
	sandboxSystemdDir := path.Dir(sandboxServicePath)
	if err := os.MkdirAll(sandboxSystemdDir, 0755); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create sandbox systemd directory")
	}

	// Copy the service file to the sandbox
	err = ti.fileManager.CopyFile(sourcePath, sandboxServicePath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy teleport.service to sandbox")
	}

	return nil
}

// patchServiceFile patches the teleport.service file with the correct binary and config paths
func (ti *teleportNodeAgentInstaller) patchServiceFile() error {
	serviceFilePath := ti.getTeleportServicePath()
	teleportBinaryPath := ti.getSandboxTeleportBinPath()

	// Replace the default binary path with our sandbox path
	// The original service file uses /usr/local/bin/teleport
	err := ti.replaceAllInFile(serviceFilePath, "/usr/local/bin/teleport", teleportBinaryPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace teleport binary path in service file")
	}

	// Replace the default config path with our config path
	// The original service file uses /etc/teleport.yaml, we use /etc/teleport/teleport.yaml
	err = ti.replaceAllInFile(serviceFilePath, "/etc/teleport.yaml", teleportConfigFile)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace config path in service file")
	}

	return nil
}

// createSystemdSymlink creates a symlink for the teleport service in the systemd directory
func (ti *teleportNodeAgentInstaller) createSystemdSymlink() error {
	teleportServicePath := ti.getTeleportServicePath()
	systemdUnitPath := ti.getSystemdUnitPath()

	err := ti.fileManager.CreateSymbolicLink(teleportServicePath, systemdUnitPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for teleport service from %s to %s", teleportServicePath, systemdUnitPath)
	}

	return nil
}

// getTeleportServicePath returns the path to the teleport.service file in the sandbox
func (ti *teleportNodeAgentInstaller) getTeleportServicePath() string {
	return path.Join(models.Paths().SandboxDir, models.SystemdUnitFilesDir, teleportServiceFileName)
}

// getSystemdUnitPath returns the path to the teleport.service file in the systemd directory
func (ti *teleportNodeAgentInstaller) getSystemdUnitPath() string {
	return path.Join(models.SystemdUnitFilesDir, teleportServiceFileName)
}

// getSandboxTeleportBinPath returns the path to the teleport binary in the sandbox
func (ti *teleportNodeAgentInstaller) getSandboxTeleportBinPath() string {
	return path.Join(models.Paths().SandboxBinDir, "teleport")
}

// validateCriticalPaths performs basic validation on critical paths used by the installer
func (ti *teleportNodeAgentInstaller) validateCriticalPaths() error {
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

// verifySandboxConfigs overrides the base implementation to verify teleport config files
// exist at their expected non-standard paths after Install() and Configure() have been called.
func (ti *teleportNodeAgentInstaller) verifySandboxConfigs() (models.StringMap, error) {
	meta := models.NewStringMap()
	requiredConfigs := map[string]string{
		"serviceFile": ti.getTeleportServicePath(), // {sandboxDir}/{models.SystemdUnitFilesDir}/teleport.service
	}

	for k, p := range requiredConfigs {
		_, exists, err := ti.fileManager.PathExists(p)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to check teleport config file at %s", p)
		}
		if !exists {
			return nil, NewFileNotFoundError(p)
		}

		meta.Set(k, p)
	}

	return meta, nil
}

// TeleportNodeAgentLeftovers returns the node agent files that make it unsafe to
// install version on a host where the agent is not fully installed: binaries
// that are not version's, and the config file, unit file and command links of
// an agent that was configured. A host with none of them is fresh, or holds an
// install of version that stopped before configuration, which install can
// finish. Installing over a configured agent would replace its patched unit
// file, and teleport configure then refuses the existing config file.
func TeleportNodeAgentLeftovers(version string) ([]string, error) {
	binaries, deployment, links, err := teleportNodeAgentFiles(version)
	if err != nil {
		return nil, err
	}
	return teleportLeftovers(binaries, deployment, links)
}

// TeleportNodeAgentConfiguration returns the config file, unit file and command
// links of a node agent that was configured, for a host where the agent is not
// fully configured. None means configuration has not started, so Configure can
// run; any of them means a configured agent has lost part of its setup.
func TeleportNodeAgentConfiguration(version string) ([]string, error) {
	_, deployment, links, err := teleportNodeAgentFiles(version)
	if err != nil {
		return nil, err
	}
	return configuredFiles(deployment, links)
}

// teleportNodeAgentFiles resolves version's binaries for this platform, the
// config and unit files, and each command link mapped to the sandbox binary it
// points to when weaver made it.
func teleportNodeAgentFiles(version string) ([]platformBinary, []string, map[string]string, error) {
	catalog, err := LoadInfrastructureCatalog()
	if err != nil {
		return nil, nil, nil, NewConfigLoadError(err)
	}
	artifact, err := catalog.GetHostArtifact(TeleportBinaryName)
	if err != nil {
		return nil, nil, nil, err
	}
	binaries, err := platformBinaries(artifact, version, func(binaryName string) string {
		return installedBinaryPath(artifact.Name, binaryName)
	})
	if err != nil {
		return nil, nil, nil, err
	}

	links := make(map[string]string, len(binaries))
	for _, b := range binaries {
		links[path.Join(models.SystemBinDir, path.Base(b.path))] = b.path
	}
	deployment := []string{teleportConfigFile, path.Join(models.SystemdUnitFilesDir, teleportServiceFileName)}
	return binaries, deployment, links, nil
}

// teleportLeftovers is the testable core of TeleportNodeAgentLeftovers.
func teleportLeftovers(binaries []platformBinary, deployment []string, links map[string]string) ([]string, error) {
	var found []string
	for _, b := range binaries {
		digest, err := fileDigest(b.path, b.algorithm)
		if errorx.IsOfType(err, FileNotFoundError) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if digest != b.checksum {
			found = append(found, b.path)
		}
	}
	configured, err := configuredFiles(deployment, links)
	if err != nil {
		return nil, err
	}
	found = append(found, configured...)
	sort.Strings(found)
	return found, nil
}

// configuredFiles is the testable core of TeleportNodeAgentConfiguration.
func configuredFiles(deployment []string, links map[string]string) ([]string, error) {
	var found []string
	for _, p := range deployment {
		if _, err := os.Lstat(p); err == nil {
			found = append(found, p)
		} else if !os.IsNotExist(err) {
			return nil, NewFileSystemError(err).WithProperty(filePathProperty, p)
		}
	}
	for link, target := range links {
		if got, err := os.Readlink(link); err == nil && got == target {
			found = append(found, link)
		}
	}
	sort.Strings(found)
	return found, nil
}
