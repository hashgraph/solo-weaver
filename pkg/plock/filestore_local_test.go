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
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestStoreUnix_Create(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)
	os.Create(filepath.Join(tmpDir, "test"))

	xs, err := newLocalFileStore(tmpDir)
	req.NoError(err)

	fi, err := xs.Create("test")
	req.NoError(err)
	req.NotNil(fi)
	req.Equal("test", fi.Name())
	req.False(fi.IsDir())
}

func TestStoreUnix_Stat(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	os.Create(filepath.Join(tmpDir, "test"))

	xs, err := newLocalFileStore(tmpDir)
	req.NoError(err)

	fi, err := xs.Stat("test")
	req.NoError(err)
	req.NotNil(fi)
	req.Equal("test", fi.Name())
	req.False(fi.IsDir())
}

func TestStoreUnix_Exists(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	os.Create(filepath.Join(tmpDir, "test"))

	xs, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	req.True(xs.Exists("test"))
}

func TestStoreUnix_Delete(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	fileName := "test"
	os.Create(filepath.Join(tmpDir, fileName))

	xs, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	req.True(xs.Exists(fileName))
	err = xs.Delete(fileName)
	req.NoError(err)
	req.False(xs.Exists(fileName))
}

func TestStoreUnix_List(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	fileName := "test"
	xs, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	fileInfo, err := xs.Create(fileName)
	req.NoError(err)

	_, err = xs.List("INVALID_DIR", "tes", -1)
	req.EqualError(err, "The argument 'dirPath' with a value of 'INVALID_DIR' is invalid: 'Could not open the dirPath'")

	list, err := xs.List(tmpDir, "tes", -1)
	req.NoError(err)
	req.Equal(1, len(list))
	req.Equal(fileInfo, list[0])
}

func TestStoreUnix_Link(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	fileName := "test"
	linkedName := "testNew"
	xs, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	fileInfo1, err := xs.Create(fileName)
	req.NoError(err)
	req.Equal(fileName, fileInfo1.Name())

	req.False(xs.Exists(linkedName))
	fileInfo2, err := xs.Link(fileName, linkedName)
	req.NoError(err)
	req.Equal(linkedName, fileInfo2.Name())
	req.True(os.SameFile(fileInfo1, fileInfo2))

	// Negative test where the files don't exist
	_, err = xs.Link("rand1", "rand2")
	req.Error(err, "Error creating a hard link of the oldFile 'rand1' using the newFile 'rand2'")
}

func TestIsValidWorkDir(t *testing.T) {
	req := require.New(t)
	xs := &localFileStore{
		workDir: ".",
	}
	req.False(xs.isValidWorkDir(".INVALID__"))
	req.False(xs.isValidWorkDir(""))

	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	req.True(xs.isValidWorkDir(tmpDir))
}

func TestValidateWorkDir_InvalidPaths(t *testing.T) {
	req := require.New(t)

	lfs := &localFileStore{
		workDir: ".",
	}

	// Validate the top-level error.  The nested cause will be printed out with the stacktrace
	// when used with the sugared error logger
	errTemplate := "The argument '%s' with a value of '%s' is invalid: 'workDir is required and must be a valid directory path'"
	err := lfs.validateWorkDir("")
	req.EqualError(err, fmt.Sprintf(errTemplate, "workDir", ""))

	err = lfs.validateWorkDir(".INVALID__")
	req.EqualError(err, fmt.Sprintf(errTemplate, "workDir", ".INVALID__"))

	//nerror.PrintErrorStacktrace(err)
}

func TestValidateWorkDir_IsDir(t *testing.T) {
	req := require.New(t)

	lfs := &localFileStore{
		workDir: ".",
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.Empty(err)
	tmpFile, err := os.CreateTemp(tmpDir, "temp.txt")
	req.Empty(err)

	errTemplate := "The argument '%s' with a value of '%s' is invalid: 'workDir is required and must be a valid directory path'"
	err = lfs.validateWorkDir(tmpFile.Name())
	req.EqualError(err, fmt.Sprintf(errTemplate, "workDir", tmpFile.Name()))

	//nerror.PrintErrorStacktrace(err)
}

func TestValidateWorkDir_CheckPermissions(t *testing.T) {
	req := require.New(t)
	lfs := &localFileStore{
		workDir: ".",
	}

	currentUser, err := user.Current()
	req.NoError(err)

	tmpDir, err := os.MkdirTemp(os.TempDir(), "check_permissions")
	req.NoError(err)
	defer func() {
		err = os.Chmod(tmpDir, 0755)
		err = os.RemoveAll(tmpDir)
	}()

	// invalid access
	err = os.Chmod(tmpDir, 0600)
	req.NoError(err)

	_, err = os.CreateTemp(tmpDir, "test")
	if currentUser.Uid != "0" { // for non-root user, there will be error
		req.Error(err)
	} else {
		req.NoError(err)
	}

	errTemplate := "The argument '%s' with a value of '%s' is invalid: 'Directory must have rwx access for the owner, found: -rw-------'"
	err = lfs.validateWorkDir(tmpDir)
	req.EqualError(err, fmt.Sprintf(errTemplate, "workDir", tmpDir))

	// check permission for group only should fail
	err = os.Chmod(tmpDir, 0070)
	req.NoError(err)

	_, err = os.CreateTemp(tmpDir, "test")
	if currentUser.Uid != "0" {
		req.Error(err) // for non-root user, there will be error
	} else {
		req.NoError(err)
	}

	err = lfs.validateWorkDir(tmpDir)
	req.Error(err)

	// check valid permission for owner
	err = os.Chmod(tmpDir, 0700)
	req.NoError(err)

	_, err = os.CreateTemp(tmpDir, "test")
	req.NoError(err)

	err = lfs.validateWorkDir(tmpDir)
	req.NoError(err)
}
