package software

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

type InstallerOption func(*baseInstaller)

func WithVersion(version string) InstallerOption {
	return func(bi *baseInstaller) {
		bi.versionToBeInstalled = version
	}
}

// baseInstaller provides common functionality for all software installers
// as well as helper functions for common operations.
type baseInstaller struct {
	downloader           *Downloader
	software             *SoftwareMetadata
	versionToBeInstalled string
	fileManager          fsx.Manager
}

var _ Software = (*baseInstaller)(nil)

// newBaseInstaller creates a new base installer with common setup
// It returns the baseInstaller struct instead of the Software interface to allow for custom implementations
// of the Software interface to access fields of the baseInstaller struct.
// For example, the kubeadm and kubelet installer needs to access the receiver functions of the baseInstaller
// struct to install and configure the .conf and .service files.
// Packages other than software, such as internal/workflows, should use the Software interface instead.
func newBaseInstaller(softwareName string, opts ...InstallerOption) (*baseInstaller, error) {
	config, err := LoadSoftwareConfig()
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	item, err := config.GetSoftwareByName(softwareName)
	if err != nil {
		return nil, err
	}

	fsxManager, err := fsx.NewManager()
	if err != nil {
		return nil, NewFileSystemError(err)
	}

	bi := &baseInstaller{
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

	return bi, nil
}

// Download handles the common download logic with checksum verification
// It downloads not only the archive file but also the config files if available.
func (b *baseInstaller) Download() error {
	downloadFolder := b.downloadFolder()

	// Create download folder if it doesn't exist
	if err := b.fileManager.CreateDirectory(downloadFolder, true); err != nil {
		return NewDownloadError(err, downloadFolder, 0)
	}

	err := b.downloadBinary()
	if err != nil {
		return err
	}

	err = b.downloadConfig()
	if err != nil {
		return err
	}

	return nil
}

func (b *baseInstaller) downloadBinary() error {
	downloadURL, err := b.software.GetDownloadURL(b.versionToBeInstalled)
	if err != nil {
		return err
	}

	destinationFile, err := b.destinationFilePath()
	if err != nil {
		return err
	}

	// Get expected checksum and algorithm from configuration
	expectedChecksum, err := b.software.GetChecksum(b.versionToBeInstalled)
	if err != nil {
		return err
	}

	// Check if file already exists and is valid
	if _, exists, err := b.fileManager.PathExists(destinationFile); err == nil && exists {
		// File exists, verify checksum
		if err := VerifyChecksum(destinationFile, expectedChecksum.Value, expectedChecksum.Algorithm); err == nil {
			// File is already downloaded and valid
			return nil
		}
		// File exists but invalid checksum, remove it and re-download
		if err := b.fileManager.RemoveAll(destinationFile); err != nil {
			return NewDownloadError(err, downloadURL, 0)
		}
	}

	// Download the file
	if err := b.downloader.Download(downloadURL, destinationFile); err != nil {
		return err
	}

	// Verify the downloaded file's checksum
	if err := VerifyChecksum(destinationFile, expectedChecksum.Value, expectedChecksum.Algorithm); err != nil {
		return err
	}

	return nil
}

func (b *baseInstaller) downloadConfig() error {
	// Get config files for kubeadm from the software configuration
	configs, err := b.software.GetConfigs(b.Version())
	if err != nil {
		return err
	}

	// Download the config file
	for _, config := range configs {
		configFile := path.Join(b.downloadFolder(), config.Name)

		// Check if file already exists and verify checksum
		_, exists, err := b.fileManager.PathExists(configFile)
		if err == nil && exists {
			// File exists, verify checksum
			if err := VerifyChecksum(configFile, config.Value, config.Algorithm); err == nil {
				// File is already downloaded and valid
				continue
			}
			// File exists but invalid checksum, remove it and re-download
			if err := b.fileManager.RemoveAll(configFile); err != nil {
				return err
			}
		}

		// Download the config file
		if err := b.downloader.Download(config.URL, configFile); err != nil {
			return err
		}

		// Verify the downloaded config file's checksum
		if err := VerifyChecksum(configFile, config.Value, config.Algorithm); err != nil {
			return err
		}
	}

	return nil
}

// Extract provides common extraction logic
func (b *baseInstaller) Extract() error {
	downloadFolder := b.downloadFolder()

	compressedFile, err := b.destinationFilePath()
	if err != nil {
		return err
	}

	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

	// Verify that the compressed file exists
	if _, exists, err := b.fileManager.PathExists(compressedFile); err != nil || !exists {
		return NewFileNotFoundError(compressedFile)
	}

	// Check if extraction folder already exists and has content
	if entries, err := os.ReadDir(extractFolder); err == nil && len(entries) > 0 {
		// Already extracted, skip
		return nil
	}

	// Create extraction directory if it doesn't exist
	if err := b.fileManager.CreateDirectory(extractFolder, true); err != nil {
		return NewExtractionError(err, compressedFile, extractFolder)
	}

	// Extract the compressed file
	if err := b.downloader.Extract(compressedFile, extractFolder); err != nil {
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

// Install provides a basic install implementation for simple binary installations
// Differently than the Download() methods, it does not handle configuration files.
func (b *baseInstaller) Install() error {
	binaryFilepath, err := b.binaryPath()
	if err != nil {
		return err
	}

	// Get sandbox bin directory from core paths
	sandboxBinDir := core.Paths().SandboxBinDir

	// Create sandbox bin directory if it doesn't exist
	err = b.fileManager.CreateDirectory(sandboxBinDir, true)
	if err != nil {
		return NewInstallationError(err, binaryFilepath, sandboxBinDir)
	}

	// Install binary to sandbox (copying directly from download location)
	binaryBasename := path.Base(binaryFilepath)
	sandboxBinary := path.Join(sandboxBinDir, binaryBasename)
	err = b.fileManager.CopyFile(binaryFilepath, sandboxBinary, true)
	if err != nil {
		return NewInstallationError(err, binaryFilepath, sandboxBinDir)
	}

	// Make the installed binary executable
	err = b.fileManager.WritePermissions(sandboxBinary, core.DefaultFilePerm, false)
	if err != nil {
		return NewInstallationError(err, binaryFilepath, sandboxBinDir)
	}

	return nil
}

// binaryPath returns the path to the main binary file after extraction
// It verifies that the binary file exists and returns an error if not found.
func (b *baseInstaller) binaryPath() (string, error) {
	binaryFilename, err := b.software.GetFilename(b.versionToBeInstalled)
	if err != nil {
		return "", err
	}

	binaryFilepath := path.Join(b.downloadFolder(), binaryFilename)

	// If the url ends with .tar.gz, we need to look into the unpacked folder
	if strings.HasSuffix(b.software.URL, ".tar.gz") {
		binaryFilepath = path.Join(b.downloadFolder(), core.DefaultUnpackFolderName, binaryFilename)
	}

	// Verify that the binary file exists
	_, exists, err := b.fileManager.PathExists(binaryFilepath)
	if err != nil || !exists {
		return "", NewFileNotFoundError(binaryFilepath)
	}

	return binaryFilepath, nil
}

// installConfig installs the config files into the sandbox at the given destination directory
// It is exported for use by the kubeadm and kubelet installers.
func (b *baseInstaller) installConfig(destinationDir string) error {
	// Verify that the config file exists
	configs, err := b.software.GetConfigs(b.Version())
	if err != nil {
		return err
	}

	err = b.fileManager.CreateDirectory(destinationDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory in sandbox", destinationDir)
	}

	// Install each config file into the sandbox
	for _, config := range configs {
		configSourcePath := path.Join(b.downloadFolder(), config.Name)
		configDestinationPath := path.Join(destinationDir, config.Name)
		err = b.fileManager.CopyFile(configSourcePath, configDestinationPath, true)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to copy %s into sandbox %s", configSourcePath, configDestinationPath)
		}
	}

	return nil
}

// Verify provides a basic verify implementation
func (b *baseInstaller) Verify() error {
	// Basic verification - check if destination file exists and is valid
	destinationFile, err := b.destinationFilePath()
	if err != nil {
		return err
	}

	if _, exists, err := b.fileManager.PathExists(destinationFile); err != nil || !exists {
		return NewFileNotFoundError(destinationFile)
	}

	// Verify checksum if available
	expectedChecksum, err := b.software.GetChecksum(b.versionToBeInstalled)
	if err != nil {
		return err
	}

	return VerifyChecksum(destinationFile, expectedChecksum.Value, expectedChecksum.Algorithm)
}

// Configure provides a basic configuration implementation
func (b *baseInstaller) Configure() error {
	sandboxBinary := path.Join(core.Paths().SandboxBinDir, b.software.Name)

	// Create symlink to /usr/local/bin for system-wide access
	systemBinary := path.Join(core.SystemBinDir, b.software.Name)

	// Create new symlink
	err := b.fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
	if err != nil {
		return NewConfigurationError(err, b.software.Name)
	}

	return nil
}

// IsConfigured provides a basic configuration check
func (b *baseInstaller) IsConfigured() (bool, error) {
	return false, nil
}

// IsInstalled provides a basic installation check
func (b *baseInstaller) IsInstalled() (bool, error) {
	destinationFile, err := b.destinationFilePath()
	if err != nil {
		return false, err
	}

	_, exists, err := b.fileManager.PathExists(destinationFile)
	return exists, err
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
	err = fileManager.WritePermissions(sourceFile, 0644, false)
	if err != nil {
		return err
	}

	return nil
}

// downloadFolder returns the download folder path for the software
func (b *baseInstaller) downloadFolder() string {
	return path.Join(core.Paths().TempDir, b.software.Name)
}

// destinationFilePath returns the full path where the downloaded file will be stored
func (b *baseInstaller) destinationFilePath() (string, error) {
	resolvedUrl, err := b.software.GetDownloadURL(b.versionToBeInstalled)
	if err != nil {
		return "", err
	}

	// Parse URL to extract the filename from its path
	parsedURL, err := url.Parse(resolvedUrl)
	if err != nil {
		return "", NewDownloadError(err, resolvedUrl, 0)
	}

	filename := path.Base(parsedURL.Path)
	if filename == "" || filename == "/" {
		return "", NewDownloadError(fmt.Errorf("no filename component in URL path: %s", resolvedUrl), resolvedUrl, 0)
	}

	return path.Join(b.downloadFolder(), filename), nil
}

// Version returns the version being installed
func (b *baseInstaller) Version() string {
	return b.versionToBeInstalled
}
