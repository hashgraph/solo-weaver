// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"os"
	"path"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
)

func CopyTemplateFile(src string, dst string) error {
	content, err := Read(src)
	if err != nil {
		return fsx.FileReadError.Wrap(err, "failed to read config file %s", src)
	}

	err = os.WriteFile(dst, content, core.DefaultDirOrExecPerm)
	if err != nil {
		return fsx.FileWriteError.Wrap(err, "failed to write config file %s", dst)
	}

	return nil
}

// CopyTemplateFiles copies configuration files from the embedded templates to the destination directory.
// It overwrites existing files in the destination directory. It returns a list of copied files.
func CopyTemplateFiles(srcDir string, destDir string) ([]string, error) {
	var copiedFiles []string
	files, err := ReadDir(srcDir)
	if err != nil {
		return copiedFiles, err
	}

	for _, src := range files {
		dst := path.Join(destDir, path.Base(src))
		err = CopyTemplateFile(src, dst)
		if err != nil {
			return copiedFiles, err
		}
		copiedFiles = append(copiedFiles, dst)
	}

	return copiedFiles, nil
}

// RemoveTemplateFiles removes configuration files from the destination directory that were copied from the source directory.
// It returns a list of removed files.
func RemoveTemplateFiles(srcDir string, destDir string) ([]string, error) {
	var removedFiles []string
	files, err := ReadDir(srcDir)
	if err != nil {
		return removedFiles, err
	}

	for _, file := range files {
		dst := path.Join(destDir, path.Base(file))
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
