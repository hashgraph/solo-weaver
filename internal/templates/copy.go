package templates

import (
	"os"
	"path"

	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

const (
	SysctlConfigTemplatesDir = "files/sysctl"
	SysctlConfigDir          = "/etc/sysctl.d"
)

// use var to allow patching in tests
var (
	sysctlConfigSourceDir      = SysctlConfigTemplatesDir
	sysctlConfigDestinationDir = SysctlConfigDir
)

func CopyFile(src string, dst string) error {
	content, err := Read(src)
	if err != nil {
		return fsx.FileReadError.Wrap(err, "failed to read config file %s", src)
	}

	err = os.WriteFile(dst, content, core.DefaultFilePerm)
	if err != nil {
		return fsx.FileWriteError.Wrap(err, "failed to write config file %s", dst)
	}

	return nil
}

// CopyFiles copies configuration files from the embedded templates to the destination directory.
// It overwrites existing files in the destination directory. It returns a list of copied files.
func CopyFiles(SrcDir string, DestDir string) ([]string, error) {
	var copiedFiles []string
	files, err := ReadDir(SrcDir)
	if err != nil {
		return copiedFiles, err
	}

	for _, file := range files {
		src := path.Join(SrcDir, file)
		dst := path.Join(DestDir, file)
		err = CopyFile(src, dst)
		if err != nil {
			return copiedFiles, err
		}
		copiedFiles = append(copiedFiles, dst)
	}

	return copiedFiles, nil
}

// CopySysctlConfigurationFiles copies sysctl configuration files from the embedded templates to the /etc/sysctl.d directory.
// It overwrites existing files in the destination directory.
func CopySysctlConfigurationFiles() ([]string, error) {
	return CopyFiles(sysctlConfigSourceDir, sysctlConfigDestinationDir)
}

func RemoveSysctlConfigurationFiles() ([]string, error) {
	var removedFiles []string
	files, err := ReadDir(sysctlConfigSourceDir)
	if err != nil {
		return removedFiles, err
	}

	for _, file := range files {
		dst := path.Join(sysctlConfigDestinationDir, file)
		if _, err = os.Stat(dst); os.IsNotExist(err) {
			continue // file does not exist, nothing to remove
		}

		// Remove the file
		err = os.Remove(dst)
		if err != nil && !os.IsNotExist(err) {
			return removedFiles, fsx.FileSystemError.Wrap(err, "failed to remove config file %s", dst)
		}
		removedFiles = append(removedFiles, dst)
	}

	return removedFiles, nil
}
