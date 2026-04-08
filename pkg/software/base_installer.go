// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// WithMachineRuntime injects a MachineRuntime into the installer so that
// IsInstalled/IsConfigured reads flow through the RSL MachineRuntimeResolver
// instead of falling back to expensive disk verification.
func WithMachineRuntime(mr MachineRuntime) InstallerOption {
	return func(bi *baseInstaller) {
		bi.machineRuntime = mr
	}
}

type InstallerOption func(*baseInstaller)

// WithVersion sets the specific version to install for the software.
// If not provided, the latest version will be used automatically.
// This is a public API option that can be used when creating installers
// to override the default version selection behavior.
func WithVersion(version string) InstallerOption {
	return func(bi *baseInstaller) {
		bi.versionToBeInstalled = version
	}
}

// baseInstaller provides common functionality for all software installers
// as well as helper functions for common operations.
type baseInstaller struct {
	name                 string
	downloader           *Downloader
	software             *ArtifactMetadata
	versionToBeInstalled string
	fileManager          fsx.Manager
	machineRuntime       MachineRuntime // optional, if it is not passed, it would perform disk check directly
	softwareState        *state.SoftwareState

	// These function fields allow for installer-specific overrides of the verification logic while still
	// providing a default implementation in the baseInstaller. For example, kubeadm and kubelet need to verify
	// configuration files in the sandbox in addition to the binaries, so they override the verifyConfigured function
	// to point to their own verification logic that includes config checks. Other software that only needs to verify
	// binaries can use the default implementation provided by baseInstaller.
	verifyInstalled  func() error
	verifyConfigured func() (models.StringMap, error)
}

var _ Software = (*baseInstaller)(nil)

// newBaseInstaller creates a new base installer with common setup
// It returns the baseInstaller struct instead of the Software interface to allow for custom implementations
// of the Software interface to access fields of the baseInstaller struct.
// For example, the kubeadm and kubelet installer needs to access the receiver functions of the baseInstaller
// struct to install and configure the .conf and .service files.
// Packages other than software, such as internal/workflows, should use the Software interface instead.
func newBaseInstaller(softwareName string, opts ...InstallerOption) (*baseInstaller, error) {
	config, err := LoadArtifactConfig()
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	item, err := config.GetArtifactByName(softwareName)
	if err != nil {
		return nil, err
	}

	fsxManager, err := fsx.NewManager()
	if err != nil {
		return nil, NewFileSystemError(err)
	}

	bi := &baseInstaller{
		name:        softwareName,
		downloader:  NewDownloader(),
		software:    item,
		fileManager: fsxManager,
	}

	for _, opt := range opts {
		opt(bi)
	}

	if bi.versionToBeInstalled == "" {
		bi.versionToBeInstalled, err = item.GetLatestVersion()
		if err != nil {
			return nil, err
		}
	}

	bi.verifyInstalled = bi.verifySandboxBinaries
	bi.verifyConfigured = bi.verifySandboxConfigs

	return bi, nil
}

// Download handles the common download logic with checksum verification
// It downloads all archives, binaries (with URLs), and config files if available.
func (b *baseInstaller) Download() error {
	downloadFolder := models.Paths().DownloadsDir

	// Create download folder if it doesn't exist
	err := b.fileManager.CreateDirectory(downloadFolder, true)
	if err != nil {
		return NewDownloadError(err, downloadFolder, 0)
	}

	err = b.downloadArchives()
	if err != nil {
		return err
	}

	err = b.downloadBinaries()
	if err != nil {
		return err
	}

	err = b.downloadConfigs()
	if err != nil {
		return err
	}

	return nil
}

// downloadArchives downloads all archives for the software version
func (b *baseInstaller) downloadArchives() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	// Download all archives
	for _, archive := range versionInfo.Archives {
		err := b.downloadAndVerifyArchive(archive)
		if err != nil {
			return err
		}
	}

	return nil
}

// downloadAndVerifyArchive downloads and verifies a single archive
func (b *baseInstaller) downloadAndVerifyArchive(archive ArchiveDetail) error {
	platform := b.software.getPlatform()

	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	// Resolve the archive URL
	downloadURL, err := executeTemplate(archive.URL, data)
	if err != nil {
		return NewTemplateError(err, b.software.Name)
	}

	// Resolve the archive name
	archiveName, err := executeTemplate(archive.Name, data)
	if err != nil {
		return NewTemplateError(err, b.software.Name)
	}

	destinationFile := path.Join(models.Paths().DownloadsDir, archiveName)

	// Get expected checksum for this archive
	osInfo, exists := archive.PlatformChecksum[platform.os]
	if !exists {
		return NewPlatformNotFoundError(b.software.Name, b.versionToBeInstalled, platform.os, "")
	}

	checksum, exists := osInfo[platform.arch]
	if !exists {
		return NewPlatformNotFoundError(b.software.Name, b.versionToBeInstalled, platform.os, platform.arch)
	}

	// Check if file already exists and is valid
	_, exists, err = b.fileManager.PathExists(destinationFile)
	if err == nil && exists {
		// File exists, verify checksum
		err = VerifyChecksum(destinationFile, checksum.Value, checksum.Algorithm)
		if err == nil {
			// File is already downloaded and valid
			return nil
		}
		// File exists but invalid checksum, remove it and re-download
		err = b.fileManager.RemoveAll(destinationFile)
		if err != nil {
			return NewDownloadError(err, downloadURL, 0)
		}
	}

	// Download the archive
	err = b.downloader.Download(downloadURL, destinationFile)
	if err != nil {
		return err
	}

	// Verify the downloaded archive's checksum
	err = VerifyChecksum(destinationFile, checksum.Value, checksum.Algorithm)
	if err != nil {
		return err
	}

	return nil
}

// downloadBinaries downloads all binaries that have URLs (not from archives)
func (b *baseInstaller) downloadBinaries() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	// Download all binaries that have URLs (not from archives)
	for _, binary := range versionInfo.BinariesByURL() {
		err := b.downloadAndVerifyBinary(binary)
		if err != nil {
			return err
		}
	}

	return nil
}

// downloadAndVerifyBinary downloads and verifies a single binary (that has a URL)
func (b *baseInstaller) downloadAndVerifyBinary(binary BinaryDetail) error {
	platform := b.software.getPlatform()

	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	// Resolve the binary URL
	downloadURL, err := executeTemplate(binary.URL, data)
	if err != nil {
		return NewTemplateError(err, b.software.Name)
	}

	// Resolve the binary name
	binaryName, err := executeTemplate(binary.Name, data)
	if err != nil {
		return NewTemplateError(err, b.software.Name)
	}

	destinationFile := path.Join(models.Paths().DownloadsDir, binaryName)

	// Get expected checksum for this binary
	osInfo, exists := binary.PlatformChecksum[platform.os]
	if !exists {
		return NewPlatformNotFoundError(b.software.Name, b.versionToBeInstalled, platform.os, "")
	}

	checksum, exists := osInfo[platform.arch]
	if !exists {
		return NewPlatformNotFoundError(b.software.Name, b.versionToBeInstalled, platform.os, platform.arch)
	}

	// Check if file already exists and is valid
	_, exists, err = b.fileManager.PathExists(destinationFile)
	if err == nil && exists {
		// File exists, verify checksum
		err = VerifyChecksum(destinationFile, checksum.Value, checksum.Algorithm)
		if err == nil {
			// File is already downloaded and valid
			return nil
		}
		// File exists but invalid checksum, remove it and re-download
		err = b.fileManager.RemoveAll(destinationFile)
		if err != nil {
			return NewDownloadError(err, downloadURL, 0)
		}
	}

	// Download the binary
	err = b.downloader.Download(downloadURL, destinationFile)
	if err != nil {
		return err
	}

	// Verify the downloaded binary's checksum
	err = VerifyChecksum(destinationFile, checksum.Value, checksum.Algorithm)
	if err != nil {
		return err
	}

	return nil
}

func (b *baseInstaller) downloadConfigs() error {
	// Get config files from the software configuration
	versionInfo, exists := b.software.Versions[Version(b.Version())]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.Version())
	}
	configs := versionInfo.ConfigsByURL()

	// Download the config file
	for _, config := range configs {
		configFile := path.Join(models.Paths().DownloadsDir, config.Name)

		// Check if file already exists and verify checksum
		_, exists, err := b.fileManager.PathExists(configFile)
		if err == nil && exists {
			// File exists, verify checksum
			err = VerifyChecksum(configFile, config.Value, config.Algorithm)
			if err == nil {
				// File is already downloaded and valid
				continue
			}
			// File exists but invalid checksum, remove it and re-download
			err = b.fileManager.RemoveAll(configFile)
			if err != nil {
				return err
			}
		}

		// Download the config file
		err = b.downloader.Download(config.URL, configFile)
		if err != nil {
			return err
		}

		// Verify the downloaded config file's checksum
		err = VerifyChecksum(configFile, config.Value, config.Algorithm)
		if err != nil {
			return err
		}
	}

	return nil
}

// Extract provides common extraction logic
func (b *baseInstaller) Extract() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	extractFolder := b.extractFolder()

	// Check if extraction folder already exists and has content
	entries, err := os.ReadDir(extractFolder)
	if err == nil && len(entries) > 0 {
		// Already extracted, verify binaries if they exist
		err = b.verifyExtractedBinaries()
		if err != nil {
			// If verification fails, clean up and re-extract
			err = b.fileManager.RemoveAll(extractFolder)
			if err != nil {
				return NewExtractionError(err, "", extractFolder)
			}
		} else {
			// Verification passed, skip extraction
			return nil
		}
	}

	// Create extraction directory if it doesn't exist
	err = b.fileManager.CreateDirectory(extractFolder, true)
	if err != nil {
		return NewExtractionError(err, "", extractFolder)
	}

	// Extract all archives
	for _, archive := range versionInfo.Archives {
		err = b.extractArchive(archive, extractFolder)
		if err != nil {
			return err
		}
	}

	// Verify checksums of extracted binaries
	err = b.verifyExtractedBinaries()
	if err != nil {
		return err
	}

	// Verify checksums of extracted configs
	err = b.verifyExtractedConfigs()
	if err != nil {
		return err
	}

	return nil
}

// extractArchive extracts a single archive
func (b *baseInstaller) extractArchive(archive ArchiveDetail, extractFolder string) error {
	platform := b.software.getPlatform()

	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	// Resolve the archive name
	archiveName, err := executeTemplate(archive.Name, data)
	if err != nil {
		return NewTemplateError(err, b.software.Name)
	}

	compressedFile := path.Join(models.Paths().DownloadsDir, archiveName)

	// Verify that the compressed file exists
	_, exists, err := b.fileManager.PathExists(compressedFile)
	if err != nil || !exists {
		return NewFileNotFoundError(compressedFile)
	}

	// Extract the compressed file
	err = b.downloader.Extract(compressedFile, extractFolder)
	if err != nil {
		return err
	}

	// Verify that extraction was successful by checking if we have files
	entries, err := os.ReadDir(extractFolder)
	if err != nil {
		return NewExtractionError(err, compressedFile, extractFolder)
	}
	if len(entries) == 0 {
		return NewExtractionError(nil, compressedFile, extractFolder)
	}

	return nil
}

// verifyExtractedBinaries verifies the checksums of all binaries extracted from an archive
func (b *baseInstaller) verifyExtractedBinaries() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	extractFolder := b.extractFolder()

	// Verify each binary that comes from an archive
	for _, binary := range versionInfo.BinariesByArchive() {
		// Resolve the binary name using template
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Get the binary's expected checksum
		osInfo, exists := binary.PlatformChecksum[platform.os]
		if !exists {
			// If platform checksum is not available, skip verification for this binary
			continue
		}

		checksum, exists := osInfo[platform.arch]
		if !exists {
			// If arch checksum is not available, skip verification for this binary
			continue
		}

		// Construct the path to the extracted binary
		binaryPath := path.Join(extractFolder, binaryName)

		// Check if the binary exists
		_, exists, err = b.fileManager.PathExists(binaryPath)
		if err != nil || !exists {
			return NewFileNotFoundError(binaryPath)
		}

		// Verify the binary's checksum
		if err := VerifyChecksum(binaryPath, checksum.Value, checksum.Algorithm); err != nil {
			return err
		}
	}

	return nil
}

// verifyExtractedConfigs verifies the checksums of all configs extracted from an archive
func (b *baseInstaller) verifyExtractedConfigs() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	extractFolder := b.extractFolder()

	// Verify each config that comes from an archive
	for _, cfg := range versionInfo.ConfigsByArchive() {
		// Resolve the config name using template
		configName, err := executeTemplate(cfg.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Construct the path to the extracted cfg
		configPath := path.Join(extractFolder, configName)

		// Check if the cfg exists
		_, exists, err = b.fileManager.PathExists(configPath)
		if err != nil || !exists {
			return NewFileNotFoundError(configPath)
		}

		// Verify the cfg's checksum
		if err := VerifyChecksum(configPath, cfg.Value, cfg.Algorithm); err != nil {
			return err
		}
	}

	return nil
}

// Install provides a basic install implementation for simple binary installations.
// It also records the installation state via the injected state.Writer.
func (b *baseInstaller) Install() error {
	err := b.performInstall()
	if err != nil {
		return err
	}
	return b.recordInstalled()
}

// performInstall performs the installation logic
func (b *baseInstaller) performInstall() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	// Get sandbox bin directory from core paths
	sandboxBinDir := models.Paths().SandboxBinDir

	// Create sandbox bin directory if it doesn't exist
	err := b.fileManager.CreateDirectory(sandboxBinDir, true)
	if err != nil {
		return NewInstallationError(err, "", sandboxBinDir)
	}

	downloadFolder := models.Paths().DownloadsDir
	extractFolder := b.extractFolder()

	// Install all binaries
	for _, binary := range versionInfo.Binaries {
		// Resolve the binary name using template
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Determine source path: check if binary comes from archive or was downloaded directly
		var sourcePath string
		if binary.Archive != "" {
			// Binary is from an archive, look in extract folder
			sourcePath = path.Join(extractFolder, binaryName)
		} else {
			// Binary was downloaded directly
			sourcePath = path.Join(downloadFolder, binaryName)
		}

		// Verify that the binary file exists
		_, exists, err := b.fileManager.PathExists(sourcePath)
		if err != nil || !exists {
			return NewFileNotFoundError(sourcePath)
		}

		// Get the base name for the destination (just the filename without path)
		binaryBasename := path.Base(binaryName)
		sandboxBinary := path.Join(sandboxBinDir, binaryBasename)

		// Copy binary to sandbox
		err = b.installFile(sourcePath, sandboxBinary, models.DefaultDirOrExecPerm)
		if err != nil {
			return NewInstallationError(err, sourcePath, sandboxBinDir)
		}
	}

	return nil
}

// installConfig installs the config files into the sandbox at the given destination directory
// It is exported for use by the kubeadm and kubelet installers.
func (b *baseInstaller) installConfig(destinationDir string) error {
	// Verify that the config file exists
	versionInfo, exists := b.software.Versions[Version(b.Version())]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.Version())
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	configs := versionInfo.GetConfigs()

	err := b.fileManager.CreateDirectory(destinationDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory in sandbox", destinationDir)
	}

	downloadFolder := models.Paths().DownloadsDir
	extractFolder := b.extractFolder()

	// Install each config file into the sandbox
	for _, config := range configs {
		// Resolve the config name using template
		configName, err := executeTemplate(config.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Determine source path: check if config comes from archive or was downloaded directly
		var sourcePath string
		if config.Archive != "" {
			// Config is from an archive, look in extract folder
			sourcePath = path.Join(extractFolder, configName)
		} else {
			// Config was downloaded directly
			sourcePath = path.Join(downloadFolder, configName)
		}

		// Verify that the config file exists
		_, exists, err := b.fileManager.PathExists(sourcePath)
		if err != nil || !exists {
			return NewFileNotFoundError(sourcePath)
		}

		configDestinationPath := path.Join(destinationDir, configName)
		err = b.fileManager.CopyFile(sourcePath, configDestinationPath, true)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to copy %s into sandbox %s", sourcePath, configDestinationPath)
		}
	}

	return nil
}

// uninstallConfig uninstalls the config files from the sandbox at the given destination directory
func (b *baseInstaller) uninstallConfig(destinationDir string) error {
	// Get config files
	versionInfo, exists := b.software.Versions[Version(b.Version())]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.Version())
	}
	configs := versionInfo.GetConfigs()

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	// Remove each config file from the sandbox
	for _, config := range configs {
		// Resolve the config name using template
		configName, err := executeTemplate(config.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		configDestinationPath := path.Join(destinationDir, configName)
		err = b.fileManager.RemoveAll(configDestinationPath)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to remove %s from sandbox", configDestinationPath)
		}
	}
	return nil
}

// Configure provides a basic configuration implementation.
// It also records the configuration state via the injected state.Writer.
func (b *baseInstaller) Configure() error {
	err := b.performConfiguration()
	if err != nil {
		return err
	}
	return b.recordConfigured()
}

// performConfiguration performs the configuration logic
func (b *baseInstaller) performConfiguration() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	sandboxBinDir := models.Paths().SandboxBinDir

	// Create symbolic links for all binaries
	for _, binary := range versionInfo.Binaries {
		// Resolve the binary name using template
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Get the base name for the destination (just the filename without path)
		binaryBasename := path.Base(binaryName)
		sandboxBinary := path.Join(sandboxBinDir, binaryBasename)
		systemBinary := path.Join(models.SystemBinDir, binaryBasename)

		// Create symlink to /usr/local/bin for system-wide access
		err = b.fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
		if err != nil {
			return NewConfigurationError(err, binaryBasename)
		}
	}

	return nil
}

// IsInstalled checks whether the software has been recorded as installed in the state.
// It refreshes from disk first so the result always reflects persisted state.
func (b *baseInstaller) IsInstalled() (bool, error) {
	if err := b.checkInstallation(); err != nil {
		return false, err
	}

	return b.softwareState.Installed, nil
}

// IsConfigured checks whether the software has been recorded as configured in the state.
// It refreshes from disk first so the result always reflects persisted state.
func (b *baseInstaller) IsConfigured() (bool, error) {
	if err := b.checkInstallation(); err != nil {
		return false, err
	}

	return b.softwareState.Configured, nil
}

// replaceAllInFile replaces all occurrences of old with new in the given file
// similar to the unix command `sed -i 's/old/new/g' file`
func (b *baseInstaller) replaceAllInFile(sourceFile string, old string, new string) error {
	fileManager := b.fileManager

	input, err := fileManager.ReadFile(sourceFile, -1)
	if err != nil {
		return err
	}

	output := strings.ReplaceAll(string(input), old, new)
	err = fileManager.WriteFile(sourceFile, []byte(output))
	if err != nil {
		return err
	}

	// rw-r--r-- permissions that are typical for config and data files
	err = fileManager.WritePermissions(sourceFile, models.DefaultFilePerm, false)
	if err != nil {
		return err
	}

	return nil
}

// extractFolder returns the software-specific extraction folder path
func (b *baseInstaller) extractFolder() string {
	return path.Join(models.Paths().TempDir, b.software.Name, models.DefaultUnpackFolderName)
}

// Version returns the version being installed
func (b *baseInstaller) Version() string {
	return b.versionToBeInstalled
}

// GetSoftwareName returns the software name
func (b *baseInstaller) GetSoftwareName() string {
	return b.software.Name
}

// Uninstall removes the software from the sandbox and cleans up related files.
// It also clears the installation state.
func (b *baseInstaller) Uninstall() error {
	err := b.performUninstall()
	if err != nil {
		return err
	}
	return nil
}

// performUninstall performs the uninstallation logic
func (b *baseInstaller) performUninstall() error {
	// Remove the binaries from the sandbox bin directory
	err := b.removeSandboxBinaries()
	if err != nil {
		return NewUninstallationError(err, b.software.Name, b.versionToBeInstalled)
	}

	return nil
}

// RemoveConfiguration restores the configuration of the software after an uninstall.
// It also clears the configuration state.
func (b *baseInstaller) RemoveConfiguration() error {
	err := b.performConfigurationRemoval()
	if err != nil {
		return err
	}
	return nil
}

// performConfigurationRemoval performs the configuration removal logic
func (b *baseInstaller) performConfigurationRemoval() error {
	err := b.cleanupSymlinks()
	if err != nil {
		return NewUninstallationError(err, b.software.Name, b.versionToBeInstalled)
	}

	return nil
}

// removeSandboxBinaries removes the binaries from the sandbox bin directory
func (b *baseInstaller) removeSandboxBinaries() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	sandboxBinDir := models.Paths().SandboxBinDir

	// Remove each binary from the sandbox bin directory
	for _, binary := range versionInfo.Binaries {
		// Resolve the binary name using template
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Get the base name for the destination (just the filename without path)
		binaryBasename := path.Base(binaryName)
		sandboxBinary := path.Join(sandboxBinDir, binaryBasename)

		// Check if binary exists in sandbox bin directory
		_, exists, err := b.fileManager.PathExists(sandboxBinary)
		if err != nil {
			continue // If we can't check, skip this one
		}
		if !exists {
			continue // Binary doesn't exist, nothing to clean up
		}

		// Remove the binary from sandbox
		err = b.fileManager.RemoveAll(sandboxBinary)
		if err != nil {
			return err
		}
	}

	return nil
}

// Cleanup performs any necessary cleanup after installation
// It only removes the extraction folder, keeping downloaded files for reuse
func (b *baseInstaller) Cleanup() error {
	extractFolder := path.Join(models.Paths().TempDir, b.software.Name)

	// Clean up only the software-specific extraction folder
	// The shared downloads folder is preserved to enable checksum-based caching
	err := b.fileManager.RemoveAll(extractFolder)
	if err != nil {
		return NewCleanupError(err, extractFolder)
	}

	return nil
}

// cleanupSymlinks removes symlinks in the system bin directory that point to our sandbox binaries
func (b *baseInstaller) cleanupSymlinks() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	sandboxBinDir := models.Paths().SandboxBinDir

	// Check each binary and remove its symlink if it exists
	for _, binary := range versionInfo.Binaries {
		// Resolve the binary name using template
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		// Get the base name for the destination (just the filename without path)
		binaryBasename := path.Base(binaryName)
		sandboxBinary := path.Join(sandboxBinDir, binaryBasename)
		systemBinary := path.Join(models.SystemBinDir, binaryBasename)

		// Check if symlink exists in system bin directory
		_, exists, err := b.fileManager.PathExists(systemBinary)
		if err != nil {
			continue // If we can't check, skip this one
		}
		if !exists {
			continue // Symlink doesn't exist, nothing to clean up
		}

		// Verify the symlink points to our sandbox binary before removing
		linkInfo, err := os.Readlink(systemBinary)
		if err != nil {
			continue // Can't read symlink, skip
		}

		// Only remove symlinks that point to our sandbox binary
		if linkInfo == sandboxBinary {
			err = b.fileManager.RemoveAll(systemBinary)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// verifySandboxBinaries verifies that all binaries are present in the sandbox bin directory
func (b *baseInstaller) verifySandboxBinaries() error {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	sandboxBinDir := models.Paths().SandboxBinDir

	for _, binary := range versionInfo.Binaries {
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, b.software.Name)
		}

		binaryBasename := path.Base(binaryName)
		sandboxBinary := path.Join(sandboxBinDir, binaryBasename)

		_, exists, err := b.fileManager.PathExists(sandboxBinary)
		if err != nil || !exists {
			return NewFileNotFoundError(sandboxBinary)
		}

		info, err := os.Stat(sandboxBinary)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to stat sandbox binary %s", sandboxBinary)
		}
		if info.Mode().Perm() != models.DefaultDirOrExecPerm {
			return errorx.IllegalState.New(
				"sandbox binary %s has wrong permissions: got %o, want %o",
				sandboxBinary, info.Mode().Perm(), models.DefaultDirOrExecPerm,
			)
		}
	}

	return nil
}

// verifySandboxConfigs verifies that system-wide symlinks in /usr/local/bin
// exist for every binary and point back to the corresponding sandbox binary.
// This mirrors what performConfiguration() creates.
// Installers with additional config files (kubelet, kubeadm, crio, cilium)
// override this with their own checks.
func (b *baseInstaller) verifySandboxConfigs() (models.StringMap, error) {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return nil, NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	platform := b.software.getPlatform()
	data := TemplateData{
		VERSION: b.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	meta := models.NewStringMap()
	sandboxBinDir := models.Paths().SandboxBinDir

	for _, binary := range versionInfo.Binaries {
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return nil, NewTemplateError(err, b.software.Name)
		}

		binaryBasename := path.Base(binaryName)
		sandboxBinary := path.Join(sandboxBinDir, binaryBasename)
		systemBinary := path.Join(models.SystemBinDir, binaryBasename)

		// Check symlink exists
		_, exists, err := b.fileManager.PathExists(systemBinary)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to check symlink at %s", systemBinary)
		}
		if !exists {
			return nil, errorx.IllegalState.New("system symlink not found at %s", systemBinary)
		}

		// Verify it points to the sandbox binary
		linkTarget, err := os.Readlink(systemBinary)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to read symlink at %s", systemBinary)
		}
		if linkTarget != sandboxBinary {
			return nil, errorx.IllegalState.New(
				"symlink %s points to %s, expected %s",
				systemBinary, linkTarget, sandboxBinary,
			)
		}

		meta.Set(binaryBasename+"SystemPath", systemBinary)
		meta.Set(binaryBasename+"SandboxPath", sandboxBinary)
	}

	return meta, nil
}

func (b *baseInstaller) VerifyInstallation() (*state.SoftwareState, error) {
	if b.verifyInstalled == nil {
		return nil, errorx.IllegalState.New("binary verification function is not set, cannot verify installation")
	}

	if b.verifyConfigured == nil {
		return nil, errorx.IllegalState.New("configuration verification function is not set, cannot verify configuration")
	}

	var err error
	softwareState := &state.SoftwareState{
		Name:       b.name,
		Version:    b.Version(),
		Installed:  false,
		Configured: false,
		LastSync:   htime.Now(),
	}

	if err = b.verifyInstalled(); err != nil {
		return softwareState, nil // if verification fails, treat as not installed
	}
	softwareState.Installed = true

	if softwareState.Metadata, err = b.verifyConfigured(); err != nil {
		return softwareState, nil // if verification fails, treat as not configured
	}
	softwareState.Configured = true

	return softwareState, nil
}

// installFile copies a file with permissions using the fileManager
// This is a helper method that can be used by any installer that needs to copy files
// with specific permissions during installation.
func (b *baseInstaller) installFile(src, dst string, perm os.FileMode) error {

	// Create destination directory if it doesn't exist
	destDir := path.Dir(dst)
	err := b.fileManager.CreateDirectory(destDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create directory %s", destDir)
	}

	// Copy file
	err = b.fileManager.CopyFile(src, dst, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy %s to %s", src, dst)
	}

	// Set permissions
	err = b.fileManager.WritePermissions(dst, perm, false)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set permissions on %s", dst)
	}

	return nil
}

func (b *baseInstaller) checkInstallation() error {
	if b.softwareState == nil {
		if b.machineRuntime != nil {
			cur, ok := b.machineRuntime.SoftwareState(b.software.Name)
			if ok {
				b.softwareState = &cur
				return nil
			}
			// ok==false means RSL not yet initialized; fall through to disk verification
		}

		var err error
		b.softwareState, err = b.VerifyInstallation()
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *baseInstaller) recordInstalled() error {
	if err := b.checkInstallation(); err != nil {
		return err
	}

	b.softwareState.Installed = true
	return nil
}

func (b *baseInstaller) clearInstalled() error {
	if err := b.checkInstallation(); err != nil {
		return err
	}

	b.softwareState.Installed = false
	return nil
}

func (b *baseInstaller) recordConfigured() error {
	if err := b.checkInstallation(); err != nil {
		return err
	}

	b.softwareState.Configured = true
	return nil
}

func (b *baseInstaller) clearConfigured() error {
	if err := b.checkInstallation(); err != nil {
		return err
	}

	b.softwareState.Configured = false
	return nil
}
