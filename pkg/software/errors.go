// SPDX-License-Identifier: Apache-2.0

package software

import (
	"strconv"

	"github.com/joomcode/errorx"
)

var (
	ErrorsNamespace       = errorx.NewNamespace("software")
	ConfigLoadError       = ErrorsNamespace.NewType("config_load_error")
	SoftwareNotFoundError = ErrorsNamespace.NewType("software_not_found")
	VersionNotFoundError  = ErrorsNamespace.NewType("version_not_found")
	PlatformNotFoundError = ErrorsNamespace.NewType("platform_not_found")
	DownloadError         = ErrorsNamespace.NewType("download_error")
	ChecksumError         = ErrorsNamespace.NewType("checksum_error")
	ExtractionError       = ErrorsNamespace.NewType("extraction_error")
	FileNotFoundError     = ErrorsNamespace.NewType("file_not_found")
	InstallationError     = ErrorsNamespace.NewType("installation_error")
	UninstallationError   = ErrorsNamespace.NewType("uninstallation_error")
	ConfigurationError    = ErrorsNamespace.NewType("configuration_error")
	CleanupError          = ErrorsNamespace.NewType("cleanup_error")
	FileSystemError       = ErrorsNamespace.NewType("filesystem_error")
	TemplateError         = ErrorsNamespace.NewType("template_error")
	PathTraversalError    = ErrorsNamespace.NewType("path_traversal_error")
	InvalidURLError       = ErrorsNamespace.NewType("invalid_url_error")

	softwareNameProperty = errorx.RegisterPrintableProperty("software_name")
	versionProperty      = errorx.RegisterPrintableProperty("versionToBeInstalled")
	urlProperty          = errorx.RegisterPrintableProperty("url")
	filePathProperty     = errorx.RegisterPrintableProperty("file_path")
	osProperty           = errorx.RegisterPrintableProperty("os")
	archProperty         = errorx.RegisterPrintableProperty("arch")
	algorithmProperty    = errorx.RegisterPrintableProperty("algorithm")
	expectedHashProperty = errorx.RegisterPrintableProperty("expected_hash")
	actualHashProperty   = errorx.RegisterPrintableProperty("actual_hash")
	statusCodeProperty   = errorx.RegisterPrintableProperty("status_code")
)

const (
	configLoadErrorMsg       = "failed to load software configuration"
	softwareNotFoundErrorMsg = "software '%s' not found in configuration"
	versionNotFoundErrorMsg  = "versionToBeInstalled '%s' not found for software '%s'"
	platformNotFoundErrorMsg = "platform '%s/%s' not supported for software '%s' versionToBeInstalled '%s'"
	downloadErrorMsg         = "failed to download from URL '%s'"
	checksumErrorMsg         = "checksum verification failed for file '%s' using algorithm '%s' [ expected = '%s', actual = '%s' ]"
	extractionErrorMsg       = "failed to extract file '%s' to '%s'"
	fileNotFoundErrorMsg     = "file not found: '%s'"
	installationErrorMsg     = "failed to install software '%s' versionToBeInstalled '%s'"
	uninstallationErrorMsg   = "failed to uninstall software '%s' versionToBeUninstalled '%s'"
	configurationErrorMsg    = "failed to configure software '%s'"
	cleanupErrorMsg          = "failed to clean up download folder %s after installation"
	filesystemErrorMsg       = "filesystem error"
	templateErrorMsg         = "failed to execute template for software '%s'"
	pathTraversalErrorMsg    = "path traversal detected: entry '%s' attempts to escape extraction directory"
	invalidURLErrorMsg       = "invalid or unsafe URL: '%s'"
)

func NewConfigLoadError(cause error) *errorx.Error {
	if cause == nil {
		return ConfigLoadError.New(configLoadErrorMsg)
	}

	return ConfigLoadError.New(configLoadErrorMsg).
		WithUnderlyingErrors(cause)
}

func NewSoftwareNotFoundError(softwareName string) *errorx.Error {
	return SoftwareNotFoundError.New(softwareNotFoundErrorMsg, softwareName).
		WithProperty(softwareNameProperty, softwareName)
}

func NewVersionNotFoundError(softwareName, version string) *errorx.Error {
	return VersionNotFoundError.New(versionNotFoundErrorMsg, version, softwareName).
		WithProperty(softwareNameProperty, softwareName).
		WithProperty(versionProperty, version)
}

func NewPlatformNotFoundError(softwareName, version, os, arch string) *errorx.Error {
	return PlatformNotFoundError.New(platformNotFoundErrorMsg, os, arch, softwareName, version).
		WithProperty(softwareNameProperty, softwareName).
		WithProperty(versionProperty, version).
		WithProperty(osProperty, os).
		WithProperty(archProperty, arch)
}

func NewDownloadError(cause error, url string, statusCode int) *errorx.Error {
	err := DownloadError.New(downloadErrorMsg, url).
		WithProperty(urlProperty, url)

	if statusCode > 0 {
		err = err.WithProperty(statusCodeProperty, statusCode)
	}

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewChecksumError(filePath, algorithm, expectedHash, actualHash string) *errorx.Error {
	return ChecksumError.New(checksumErrorMsg, filePath, algorithm, expectedHash, actualHash).
		WithProperty(filePathProperty, filePath).
		WithProperty(algorithmProperty, algorithm).
		WithProperty(expectedHashProperty, expectedHash).
		WithProperty(actualHashProperty, actualHash)
}

func NewExtractionError(cause error, filePath, destPath string) *errorx.Error {
	err := ExtractionError.New(extractionErrorMsg, filePath, destPath).
		WithProperty(filePathProperty, filePath)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewFileNotFoundError(filePath string) *errorx.Error {
	return FileNotFoundError.New(fileNotFoundErrorMsg, filePath).
		WithProperty(filePathProperty, filePath)
}

func NewInstallationError(cause error, softwareName, version string) *errorx.Error {
	err := InstallationError.New(installationErrorMsg, softwareName, version).
		WithProperty(softwareNameProperty, softwareName).
		WithProperty(versionProperty, version)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewUninstallationError(cause error, softwareName, version string) *errorx.Error {
	err := UninstallationError.New(uninstallationErrorMsg, softwareName, version).
		WithProperty(softwareNameProperty, softwareName).
		WithProperty(versionProperty, version)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewConfigurationError(cause error, softwareName string) *errorx.Error {
	err := ConfigurationError.New(configurationErrorMsg, softwareName).
		WithProperty(softwareNameProperty, softwareName)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewCleanupError(cause error, downloadFolder string) *errorx.Error {
	err := CleanupError.New(cleanupErrorMsg, downloadFolder).
		WithProperty(filePathProperty, downloadFolder)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewFileSystemError(cause error) *errorx.Error {
	return FileSystemError.New(filesystemErrorMsg).
		WithUnderlyingErrors(cause)
}

func NewTemplateError(cause error, softwareName string) *errorx.Error {
	err := TemplateError.New(templateErrorMsg, softwareName).
		WithProperty(softwareNameProperty, softwareName)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

func NewPathTraversalError(entryName string) *errorx.Error {
	return PathTraversalError.New(pathTraversalErrorMsg, entryName).
		WithProperty(filePathProperty, entryName)
}

func NewInvalidURLError(cause error, url string) *errorx.Error {
	err := InvalidURLError.New(invalidURLErrorMsg, url).
		WithProperty(urlProperty, url)

	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}

	return err
}

// SafeErrorDetails emits a PII-safe slice of error details.
func SafeErrorDetails(err *errorx.Error) []string {
	var safeDetails []string
	if err == nil {
		return safeDetails
	}

	for _, prop := range []errorx.Property{
		softwareNameProperty, versionProperty, urlProperty, filePathProperty,
		osProperty, archProperty, algorithmProperty, expectedHashProperty,
		actualHashProperty, statusCodeProperty,
	} {
		if val, ok := err.Property(prop); ok {
			switch prop {
			case softwareNameProperty, versionProperty, urlProperty, filePathProperty,
				osProperty, archProperty, algorithmProperty, expectedHashProperty, actualHashProperty:
				safeDetails = append(safeDetails, val.(string))
			case statusCodeProperty:
				safeDetails = append(safeDetails, strconv.Itoa(val.(int)))
			}
		}
	}

	return safeDetails
}
