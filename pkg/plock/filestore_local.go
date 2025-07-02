/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package plock

import (
	"github.com/joomcode/errorx"
	"os"
	"path/filepath"
	"strings"
)

// localFileStore is the default implementation of fileStore api for local file system
type localFileStore struct {
	providerType string
	workDir      string
}

// newLocalFileStore initializes and returns an instance of local file store
func newLocalFileStore(workDir string) (fileStore, error) {
	// just init an instance so that we can call other validation methods
	lfs := &localFileStore{
		providerType: ProviderLocal,
		workDir:      ".",
	}

	// set work directory
	err := lfs.SetWorkDir(workDir)
	if err != nil {
		return nil, err
	}

	return lfs, nil
}

// ProviderType returns the storage provider type
func (lfs *localFileStore) ProviderType() string {
	return lfs.providerType
}

// isValidWorkdir check if the directory exists and writable
// It ensures the directory has read-write access for user or all is accessible for the user or public
func (lfs *localFileStore) isValidWorkDir(workDir string) bool {
	if workDir == "" {
		return false
	}

	dirInfo, err := os.Stat(workDir)
	if err != nil {
		return false
	}

	permission := dirInfo.Mode().Perm()
	if dirInfo.IsDir() &&
		(permission&0o600 != 0 || permission&0o006 != 0) { // rw access for user or all
		return true
	}

	return false
}

func (lfs *localFileStore) validateWorkDir(workDir string) error {

	dirInfo, err := os.Stat(workDir)
	if err != nil {
		return errorx.IllegalArgument.
			New("workDir is required and must be a valid directory path: %s", workDir).WithUnderlyingErrors(err)
	}

	if !dirInfo.IsDir() {
		return errorx.IllegalArgument.New("workDir is required and must be a valid directory path: %s", workDir)
	}

	permission := dirInfo.Mode().Perm()
	if permission&0700 == 0700 { // rwx access for owner allow to create and delete files
		return nil
	}

	return errorx.IllegalArgument.
		New("Directory '%s' must have rwx access for the owner, found: %s", workDir, permission.String())

}

// SetWorkDir sets the work directory
func (lfs *localFileStore) SetWorkDir(dirName string) error {
	dirName = filepath.Clean(dirName)

	err := lfs.validateWorkDir(dirName)
	if err != nil {
		return errorx.IllegalArgument.New("invalid work directory: %s", dirName).WithUnderlyingErrors(err)
	}

	lfs.workDir = dirName

	return nil
}

// GetWorkDir returns the current work directory
func (lfs *localFileStore) GetWorkDir() string {
	return lfs.workDir
}

// Stat returns os.FileInfo of the named file
func (lfs *localFileStore) Stat(fileName string) (os.FileInfo, error) {
	path := lfs.FullPath(fileName)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// Exists returns true if the file exists
func (lfs *localFileStore) Exists(fileName string) bool {
	info, _ := lfs.Stat(fileName)
	return info != nil
}

// Create creates a new file
func (lfs *localFileStore) Create(fileName string) (os.FileInfo, error) {
	path := lfs.FullPath(fileName)
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	f.Close()

	return lfs.Stat(fileName)
}

// Delete deletes the file
func (lfs *localFileStore) Delete(fileName string) error {
	path := lfs.FullPath(fileName)
	return os.Remove(path)
}

// Link creates a hard link of the oldFile using the newFile
func (lfs *localFileStore) Link(oldFile string, newFile string) (os.FileInfo, error) {
	old := lfs.FullPath(oldFile)
	fullPath := lfs.FullPath(newFile)
	err := os.Link(old, fullPath)
	if err != nil {
		return nil, errorx.IllegalState.
			New("Error creating a hard link of the oldFile '%s' using the newFile '%s'", oldFile, newFile).
			WithUnderlyingErrors(err)
	}

	fileInfo, err := lfs.Stat(newFile)
	if err != nil {
		return nil, errorx.IllegalState.
			New("Error fetching the fileInfo for newFile '%s'", newFile).
			WithUnderlyingErrors(err)
	}
	return fileInfo, nil
}

// List returns a list of files in the specified directory
// Note this is a costly operation and should be used with care
//
// If substr is not an empty string it tries to find file names that contains the substr
// if maxCount <= 0, it returns all files in the directory
// if maxCount > 0, it returns at most maxCount number of files in the directory, if exists
func (lfs *localFileStore) List(dirPath string, substr string, maxCount int) ([]os.FileInfo, error) {
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, errorx.IllegalArgument.New("Could not open the dirPath: %s", dirPath).WithUnderlyingErrors(err)
	}
	defer dir.Close()

	if maxCount < 0 {
		maxCount = 0 // 0 means all files
	}
	fileList, _ := dir.Readdirnames(maxCount)

	// apply filter
	var list []os.FileInfo
	for _, fileName := range fileList {
		if strings.Contains(fileName, substr) {
			info, err := lfs.Stat(fileName)
			if err == nil {
				list = append(list, info)
			}
		}
	}

	return list, nil
}

// FullPath returns full path combining fileName and dirName
func (lfs *localFileStore) FullPath(fileName string) string {
	return filepath.Join(lfs.workDir, fileName)
}
