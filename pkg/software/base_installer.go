package software

import (
	"os"
	"path"

	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

// BaseInstaller provides common functionality for all software installers
type BaseInstaller struct {
	downloader            *Downloader
	softwareToBeInstalled *SoftwareMetadata
	versionToBeInstalled  string
	fileManager           fsx.Manager
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
		downloader:            NewDownloader(),
		softwareToBeInstalled: item,
		fileManager:           fsxManager,
		versionToBeInstalled:  selectedVersion,
	}, nil
}

// Download handles the common download logic with checksum verification
func (b *BaseInstaller) Download() error {
	downloadFolder := b.GetDownloadFolder()

	// Create download folder if it doesn't exist
	if err := b.fileManager.CreateDirectory(downloadFolder, true); err != nil {
		return NewDownloadError(err, downloadFolder, 0)
	}

	downloadURL, err := b.softwareToBeInstalled.GetDownloadURL(b.versionToBeInstalled)
	if err != nil {
		return err
	}

	destinationFile, err := b.GetDestinationFile()
	if err != nil {
		return err
	}

	// Get expected checksum and algorithm from configuration
	expectedChecksum, err := b.softwareToBeInstalled.GetChecksum(b.versionToBeInstalled)
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

// Extract provides common extraction logic
func (b *BaseInstaller) Extract() error {
	downloadFolder := b.GetDownloadFolder()

	compressedFile, err := b.GetDestinationFile()
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

// GetDownloadFolder returns the download folder path for the software
func (b *BaseInstaller) GetDownloadFolder() string {
	return path.Join(core.Paths().TempDir, b.softwareToBeInstalled.Name)
}

// GetDestinationFile returns the full path where the downloaded file will be stored
func (b *BaseInstaller) GetDestinationFile() (string, error) {
	filename, err := b.softwareToBeInstalled.GetFilename(b.versionToBeInstalled)
	if err != nil {
		return "", err
	}
	return path.Join(b.GetDownloadFolder(), filename), nil
}

// GetVersion returns the versionToBeInstalled being installed
func (b *BaseInstaller) GetVersion() string {
	return b.versionToBeInstalled
}

// GetMetadata returns the software softwareToBeInstalled
func (b *BaseInstaller) GetMetadata() *SoftwareMetadata {
	return b.softwareToBeInstalled
}

// GetFileManager returns the file manager instance
func (b *BaseInstaller) GetFileManager() fsx.Manager {
	return b.fileManager
}

// GetDownloader returns the downloader instance
func (b *BaseInstaller) GetDownloader() *Downloader {
	return b.downloader
}

// Install provides a basic install implementation for simple binary installations
func (b *BaseInstaller) Install() error {
	binaryFile, err := b.GetDestinationFile()
	if err != nil {
		return err
	}

	fileManager := b.GetFileManager()

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
	sandboxBinary := path.Join(sandboxBinDir, b.softwareToBeInstalled.Name)
	err = fileManager.CopyFile(binaryFile, sandboxBinary, true)
	if err != nil {
		return NewInstallationError(err, binaryFile, sandboxBinDir)
	}

	// Make the installed binary executable
	err = fileManager.WritePermissions(sandboxBinary, 0755, false)
	if err != nil {
		return NewExtractionError(err, binaryFile, "")
	}

	return nil
}

// Verify provides a basic verify implementation
func (b *BaseInstaller) Verify() error {
	// Basic verification - check if destination file exists and is valid
	destinationFile, err := b.GetDestinationFile()
	if err != nil {
		return err
	}

	if _, exists, err := b.fileManager.PathExists(destinationFile); err != nil || !exists {
		return NewFileNotFoundError(destinationFile)
	}

	// Verify checksum if available
	expectedChecksum, err := b.softwareToBeInstalled.GetChecksum(b.versionToBeInstalled)
	if err != nil {
		return err
	}

	return VerifyChecksum(destinationFile, expectedChecksum.Value, expectedChecksum.Algorithm)
}

// IsInstalled provides a basic installation check
func (b *BaseInstaller) IsInstalled() (bool, error) {
	destinationFile, err := b.GetDestinationFile()
	if err != nil {
		return false, err
	}

	_, exists, err := b.fileManager.PathExists(destinationFile)
	return exists, err
}
