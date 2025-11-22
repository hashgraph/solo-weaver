package software

import (
	"os"
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/state"
	"golang.hedera.com/solo-weaver/pkg/fsx"
)

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
	downloader           *Downloader
	software             *ArtifactMetadata
	versionToBeInstalled string
	fileManager          fsx.Manager
	stateManager         *state.Manager
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
		downloader:   NewDownloader(),
		software:     item,
		fileManager:  fsxManager,
		stateManager: state.NewManager(fsxManager),
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

	return bi, nil
}

// Download handles the common download logic with checksum verification
// It downloads all archives, binaries (with URLs), and config files if available.
func (b *baseInstaller) Download() error {
	downloadFolder := b.downloadFolder()

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

	destinationFile := path.Join(b.downloadFolder(), archiveName)

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

	destinationFile := path.Join(b.downloadFolder(), binaryName)

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
		configFile := path.Join(b.downloadFolder(), config.Name)

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

	downloadFolder := b.downloadFolder()
	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

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

	compressedFile := path.Join(b.downloadFolder(), archiveName)

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

	downloadFolder := b.downloadFolder()
	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

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

	downloadFolder := b.downloadFolder()
	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

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
func (b *baseInstaller) Install() error {
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
	sandboxBinDir := core.Paths().SandboxBinDir

	// Create sandbox bin directory if it doesn't exist
	err := b.fileManager.CreateDirectory(sandboxBinDir, true)
	if err != nil {
		return NewInstallationError(err, "", sandboxBinDir)
	}

	downloadFolder := b.downloadFolder()
	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

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
		err = b.installFile(sourcePath, sandboxBinary, core.DefaultDirOrExecPerm)
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

	downloadFolder := b.downloadFolder()
	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

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
func (b *baseInstaller) Configure() error {
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

	sandboxBinDir := core.Paths().SandboxBinDir

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
		systemBinary := path.Join(core.SystemBinDir, binaryBasename)

		// Create symlink to /usr/local/bin for system-wide access
		err = b.fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
		if err != nil {
			return NewConfigurationError(err, binaryBasename)
		}
	}

	return nil
}

// IsInstalled provides a basic installation check
func (b *baseInstaller) IsInstalled() (bool, error) {
	return b.stateManager.Exists(b.software.Name, state.TypeInstalled)
}

// IsConfigured provides a basic configuration check
func (b *baseInstaller) IsConfigured() (bool, error) {
	return b.stateManager.Exists(b.software.Name, state.TypeConfigured)
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
	err = fileManager.WritePermissions(sourceFile, core.DefaultFilePerm, false)
	if err != nil {
		return err
	}

	return nil
}

// downloadFolder returns the download folder path for the software
func (b *baseInstaller) downloadFolder() string {
	return path.Join(core.Paths().TempDir, b.software.Name)
}

// Version returns the version being installed
func (b *baseInstaller) Version() string {
	return b.versionToBeInstalled
}

// GetStateManager returns the state manager for external state management
func (b *baseInstaller) GetStateManager() *state.Manager {
	return b.stateManager
}

// GetSoftwareName returns the software name
func (b *baseInstaller) GetSoftwareName() string {
	return b.software.Name
}

// Uninstall removes the software from the sandbox and cleans up related files.
func (b *baseInstaller) Uninstall() error {
	// Remove the binaries from the sandbox bin directory
	err := b.removeSandboxBinaries()
	if err != nil {
		return NewUninstallationError(err, b.software.Name, b.versionToBeInstalled)
	}

	return nil
}

// RemoveConfiguration restores the configuration of the software after an uninstall.
func (b *baseInstaller) RemoveConfiguration() error {
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

	sandboxBinDir := core.Paths().SandboxBinDir

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
func (b *baseInstaller) Cleanup() error {
	downloadFolder := b.downloadFolder()

	// Clean up download and extract folders if installation succeeded
	err := b.fileManager.RemoveAll(downloadFolder)
	if err != nil {
		return NewCleanupError(err, downloadFolder)
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

	sandboxBinDir := core.Paths().SandboxBinDir

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
		systemBinary := path.Join(core.SystemBinDir, binaryBasename)

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
