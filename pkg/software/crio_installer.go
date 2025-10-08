package software

import (
	"os"
	"path"

	"golang.hedera.com/solo-provisioner/internal/core"
)

type crioInstaller struct {
	downloader            *Downloader
	softwareToBeInstalled *SoftwareItem
	version               string
}

var _ Software = (*crioInstaller)(nil)

func NewCrioInstaller() (Software, error) {
	config, err := LoadSoftwareConfig()
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	item, err := config.GetSoftwareByName("cri-o")
	if err != nil {
		return nil, err // Already using NewSoftwareNotFoundError
	}

	// Get latest version
	version, err := item.GetLatestVersion()
	if err != nil {
		return nil, err // Already using NewVersionNotFoundError
	}

	return &crioInstaller{
		downloader:            NewDownloader(),
		softwareToBeInstalled: item,
		version:               version,
	}, nil
}

func (ci *crioInstaller) Download() error {
	_, err := ci.softwareToBeInstalled.GetFilename(ci.version)
	if err != nil {
		return err // Already using NewTemplateError
	}

	downloadFolder := ci.getDownloadFolder()

	downloadURL, err := ci.softwareToBeInstalled.GetDownloadURL(ci.version)
	if err != nil {
		return err // Already using NewTemplateError
	}

	destinationFile, err := ci.getDestinationFile()
	if err != nil {
		return err // Already using NewTemplateError
	}

	// Get expected checksum and algorithm from configuration
	expectedChecksum, err := ci.softwareToBeInstalled.GetChecksum(ci.version)
	if err != nil {
		return err // Already using structured errors
	}

	// Create download folder if it doesn't exist
	if err := os.MkdirAll(downloadFolder, core.DefaultFilePerm); err != nil {
		return NewDownloadError(err, downloadURL, 0)
	}

	// Check if file already exists and is valid
	if _, err := os.Stat(destinationFile); err == nil {
		// File exists, verify checksum
		if err := ci.downloader.VerifyChecksum(destinationFile, expectedChecksum.Value, expectedChecksum.Algorithm); err == nil {
			// File is already downloaded and valid
			return nil
		}
		// File exists but invalid checksum, remove it and re-download
		if err := os.Remove(destinationFile); err != nil {
			return NewDownloadError(err, downloadURL, 0)
		}
	}

	// Download the file
	if err := ci.downloader.Download(downloadURL, destinationFile); err != nil {
		return err // Already using NewDownloadError
	}

	// Verify the downloaded file's checksum using the dynamic algorithm
	if err := ci.downloader.VerifyChecksum(destinationFile, expectedChecksum.Value, expectedChecksum.Algorithm); err != nil {
		return err // Already using NewChecksumError
	}

	return nil
}

func (ci *crioInstaller) Extract() error {
	downloadFolder := ci.getDownloadFolder()

	compressedFile, err := ci.getDestinationFile()
	if err != nil {
		return err // Already using NewTemplateError
	}

	extractFolder := path.Join(downloadFolder, core.DefaultUnpackFolderName)

	// Verify that the compressed file exists
	if _, err := os.Stat(compressedFile); os.IsNotExist(err) {
		return NewFileNotFoundError(compressedFile)
	}

	// Check if extraction folder already exists and has content
	if entries, err := os.ReadDir(extractFolder); err == nil && len(entries) > 0 {
		// Already extracted, skip
		return nil
	}

	// Create extraction folder if it doesn't exist
	if err := os.MkdirAll(extractFolder, core.DefaultFilePerm); err != nil {
		return NewExtractionError(err, compressedFile, extractFolder)
	}

	// Extract the compressed file
	if err := ci.downloader.Extract(compressedFile, extractFolder); err != nil {
		return err // Already using NewExtractionError
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

func (ci *crioInstaller) Install() error {

	// mv to sandbox

	// installation/binary symlink

	return nil
}

// Verify performs binary integrity check
func (ci *crioInstaller) Verify() error {
	return nil
}

// Checks the directories and highlevel contents in sandbox
// and checks integrity/existence of binary symlink
func (ci *crioInstaller) IsInstalled() (bool, error) {
	return false, nil
}

func (ci *crioInstaller) Configure() error {
	// default configuration
	//	/etc/default/crio

	// service configuration
	//	/usr/lib/systemd/system/crio.service

	// application configuration
	// 	/etc/crio/crio.conf.d

	// configuration service symlink
	// 	/usr/lib/systemd/system/crio.service

	return nil
}

// Checks default, service, application and configuration service symlinks
func (ci *crioInstaller) IsConfigured() (bool, error) {
	return false, nil
}

// getDownloadFolder returns the download folder path for cri-o
func (ci *crioInstaller) getDownloadFolder() string {
	return path.Join(core.Paths().TempDir, "cri-o")
}

// getDestinationFile returns the full path where the downloaded file will be stored
func (ci *crioInstaller) getDestinationFile() (string, error) {
	filename, err := ci.softwareToBeInstalled.GetFilename(ci.version)
	if err != nil {
		return "", err
	}
	return path.Join(ci.getDownloadFolder(), filename), nil
}
