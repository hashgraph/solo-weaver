package software

import (
	"os"
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

// BaseInstaller provides common functionality for all software installers
type BaseInstaller struct {
	downloader           *Downloader
	software             *SoftwareMetadata
	versionToBeInstalled string
	fileManager          fsx.Manager
}

// NewBaseInstaller creates a new base installer with common setup
func NewBaseInstaller(softwareName, selectedVersion string) (*BaseInstaller, error) {
	config, err := LoadSoftwareConfig()
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	item, err := config.GetSoftwareByName(softwareName)
	if err != nil {
		return nil, err
	}

	if selectedVersion == "" {
		selectedVersion, err = item.GetLatestVersion()
		if err != nil {
			return nil, err
		}
	}

	fsxManager, err := fsx.NewManager()
	if err != nil {
		return nil, NewFileSystemError(err)
	}

	return &BaseInstaller{
		downloader:           NewDownloader(),
		software:             item,
		fileManager:          fsxManager,
		versionToBeInstalled: selectedVersion,
	}, nil
}

// Download handles the common download logic with checksum verification
func (b *BaseInstaller) Download() error {
	downloadFolder := b.DownloadFolder()

	// Create download folder if it doesn't exist
	if err := b.fileManager.CreateDirectory(downloadFolder, true); err != nil {
		return NewDownloadError(err, downloadFolder, 0)
	}

	downloadURL, err := b.software.GetDownloadURL(b.versionToBeInstalled)
	if err != nil {
		return err
	}

	destinationFile, err := b.DestinationFile()
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

func (b *BaseInstaller) DownloadConfigFiles() error {
	// Get config files for kubeadm from the software configuration
	configs, err := b.Software().GetConfigs(b.Version())
	if err != nil {
		return err
	}

	// Download the config file
	for _, config := range configs {
		configFile := path.Join(b.DownloadFolder(), config.Filename)

		// Check if file already exists and verify checksum
		_, exists, err := b.FileManager().PathExists(configFile)
		if err == nil && exists {
			// File exists, verify checksum
			if err := VerifyChecksum(configFile, config.Value, config.Algorithm); err == nil {
				// File is already downloaded and valid
				continue
			}
			// File exists but invalid checksum, remove it and re-download
			if err := b.FileManager().RemoveAll(configFile); err != nil {
				return err
			}
		}

		// Download the config file
		if err := b.Downloader().Download(config.URL, configFile); err != nil {
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
func (b *BaseInstaller) Extract() error {
	downloadFolder := b.DownloadFolder()

	compressedFile, err := b.DestinationFile()
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
func (b *BaseInstaller) Install() error {
	binaryFile, err := b.DestinationFile()
	if err != nil {
		return err
	}

	fileManager := b.FileManager()

	// Verify that the binary file exists
	_, exists, err := fileManager.PathExists(binaryFile)
	if err != nil || !exists {
		return NewFileNotFoundError(binaryFile)
	}

	// Get sandbox bin directory from core paths
	sandboxBinDir := core.Paths().SandboxBinDir

	// Create sandbox bin directory if it doesn't exist
	err = fileManager.CreateDirectory(sandboxBinDir, true)
	if err != nil {
		return NewInstallationError(err, binaryFile, sandboxBinDir)
	}

	// Install binary to sandbox (copying directly from download location)
	sandboxBinary := path.Join(sandboxBinDir, b.software.Name)
	err = fileManager.CopyFile(binaryFile, sandboxBinary, true)
	if err != nil {
		return NewInstallationError(err, binaryFile, sandboxBinDir)
	}

	// Make the installed binary executable
	err = fileManager.WritePermissions(sandboxBinary, core.DefaultFilePerm, false)
	if err != nil {
		return NewExtractionError(err, binaryFile, "")
	}

	return nil
}

func (b *BaseInstaller) InstallConfigFiles(destinationDir string) error {
	// Verify that the config file exists
	configs, err := b.Software().GetConfigs(b.Version())
	if err != nil {
		return err
	}

	// Install each config file into the sandbox
	for _, config := range configs {
		err = b.FileManager().CreateDirectory(destinationDir, true)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create %s directory in sandbox", destinationDir)
		}

		configSourcePath := path.Join(b.DownloadFolder(), config.Filename)
		configDestinationPath := path.Join(destinationDir, config.Filename)
		err = b.FileManager().CopyFile(configSourcePath, configDestinationPath, true)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to copy %s into sandbox %s", configSourcePath, configDestinationPath)
		}
	}

	return nil
}

// Verify provides a basic verify implementation
func (b *BaseInstaller) Verify() error {
	// Basic verification - check if destination file exists and is valid
	destinationFile, err := b.DestinationFile()
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

// IsInstalled provides a basic installation check
func (b *BaseInstaller) IsInstalled() (bool, error) {
	destinationFile, err := b.DestinationFile()
	if err != nil {
		return false, err
	}

	_, exists, err := b.fileManager.PathExists(destinationFile)
	return exists, err
}

// replaceAllInFile replaces all occurrences of old with new in the given file
// similar to the unix command `sed -i 's/old/new/g' file`
func (b *BaseInstaller) replaceAllInFile(sourceFile string, old string, new string) error {
	fileManager := b.FileManager()

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

// DownloadFolder returns the download folder path for the software
func (b *BaseInstaller) DownloadFolder() string {
	return path.Join(core.Paths().TempDir, b.software.Name)
}

// DestinationFile returns the full path where the downloaded file will be stored
func (b *BaseInstaller) DestinationFile() (string, error) {
	filename, err := b.software.GetFilename(b.versionToBeInstalled)
	if err != nil {
		return "", err
	}
	return path.Join(b.DownloadFolder(), filename), nil
}

// Version returns the version being installed
func (b *BaseInstaller) Version() string {
	return b.versionToBeInstalled
}

// Software returns the software metadata
func (b *BaseInstaller) Software() *SoftwareMetadata {
	return b.software
}

// FileManager returns the file manager instance
func (b *BaseInstaller) FileManager() fsx.Manager {
	return b.fileManager
}

// Downloader returns the downloader instance
func (b *BaseInstaller) Downloader() *Downloader {
	return b.downloader
}
