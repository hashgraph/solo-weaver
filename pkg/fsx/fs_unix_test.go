/*
 * Copyright (C) 2021-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package fsx

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/golang/mock/gomock"
	"github.com/joomcode/errorx"
	assertions "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/security"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
)

func chmodPermNotationToFileMode(perms string) fs.FileMode {
	mode, err := strconv.ParseUint(perms, 8, 32)
	if err != nil {
		panic(err)
	}

	return fs.FileMode(mode)
}

func assertFileOwnership(t *testing.T, fsManager Manager, path string, usr principal.User, grp principal.Group) {
	t.Helper()
	assert := assertions.New(t)

	readUser, readGroup, err := fsManager.ReadOwner(path)
	assert.NoError(err)
	assert.Equal(usr.Uid(), readUser.Uid())
	assert.Equal(grp.Gid(), readGroup.Gid())
}

func setupTest(t *testing.T, pm principal.Manager) (*assertions.Assertions, Manager) {
	t.Helper()
	assert := assertions.New(t)

	manager, err := NewManager(WithPrincipalManager(pm))
	assert.NoError(err)

	return assert, manager
}

func setupMockPrincipalManager(t *testing.T, ctrl *gomock.Controller) principal.Manager {
	pm := principal.NewMockManager(ctrl)
	assert := assertions.New(t)

	currentUser, err := user.Current()
	assert.NoError(err)

	mockUser := principal.NewMockUser(ctrl)
	grp := principal.NewMockGroup(ctrl)
	mockUser.EXPECT().Uid().Return(currentUser.Uid).AnyTimes()
	mockUser.EXPECT().Name().Return(currentUser.Name).AnyTimes()
	grp.EXPECT().Gid().Return(currentUser.Gid).AnyTimes()
	mockUser.EXPECT().PrimaryGroup().Return(grp).AnyTimes()

	pm.EXPECT().LookupUserById(currentUser.Uid).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupUserByName(currentUser.Username).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupGroupById(currentUser.Gid).Return(grp, nil).AnyTimes()

	return pm
}

func setupMockPrincipalManagerForHedera(t *testing.T, ctrl *gomock.Controller) principal.Manager {
	pm := principal.NewMockManager(ctrl)
	assert := assertions.New(t)

	currentUser, err := user.Current()
	assert.NoError(err)

	mockUser := principal.NewMockUser(ctrl)
	grp := principal.NewMockGroup(ctrl)
	mockUser.EXPECT().Uid().Return(currentUser.Uid).AnyTimes()
	mockUser.EXPECT().Name().Return(currentUser.Name).AnyTimes()
	grp.EXPECT().Gid().Return(currentUser.Gid).AnyTimes()
	mockUser.EXPECT().PrimaryGroup().Return(grp).AnyTimes()

	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupGroupByName(security.ServiceAccountGroupName).Return(grp, nil).AnyTimes()

	return pm
}

func TestNewManager(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	assert.NotNil(manager)
	assert.IsType(&unixManager{}, manager)
}

func TestUnixManager_PathExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	fi, exists, err := manager.PathExists(tmpDir)
	assert.NoError(err)
	assert.True(exists)
	assert.NotNil(fi)

	fi, exists, err = manager.PathExists(path.Join(tmpDir, "non-existent"))
	assert.NoError(err)
	assert.False(exists)
	assert.Nil(fi)
}

func TestUnixManager_IsDirectory(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	isDir := manager.IsDirectory(tmpDir)
	assert.True(isDir)

	isDir = manager.IsDirectory(path.Join(tmpDir, "non-existent"))
	assert.False(isDir)

	isDir = manager.IsDirectory("")
	assert.False(isDir)
}

func TestUnixManager_IsFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	tmpFile, err := os.CreateTemp(tmpDir, "test-file.*")
	if assert.NoError(err) {
		defer Remove(tmpFile.Name())
	}

	isFile := manager.IsRegularFile(path.Join(tmpDir, "non-existent"))
	assert.False(isFile)

	isFile = manager.IsRegularFile("")
	assert.False(isFile)

	isFile = manager.IsRegularFile(tmpFile.Name())
	assert.True(isFile)
}

func TestUnixManager_IsHardLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	tmpFile, err := os.CreateTemp(tmpDir, "test-file.*")
	if assert.NoError(err) {
		defer Remove(tmpFile.Name())
	}

	isHardLink := manager.IsHardLink(path.Join(tmpDir, "non-existent"))
	assert.False(isHardLink)

	isHardLink = manager.IsHardLink("")
	assert.False(isHardLink)

	isHardLink = manager.IsHardLink(tmpFile.Name())
	assert.False(isHardLink)

	// Create a hard link to the file.
	hardLinkPath := path.Join(tmpDir, "hard-link")
	err = os.Link(tmpFile.Name(), hardLinkPath)
	if assert.NoError(err) {
		defer Remove(hardLinkPath)
	}

	isHardLink = manager.IsHardLink(hardLinkPath)
	assert.True(isHardLink)
}

func TestUnixManager_IsSymbolicLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	tmpFile, err := os.CreateTemp(tmpDir, "test-file.*")
	if assert.NoError(err) {
		defer Remove(tmpFile.Name())
	}

	isSymbolicLink := manager.IsSymbolicLink(path.Join(tmpDir, "non-existent"))
	assert.False(isSymbolicLink)

	isSymbolicLink = manager.IsSymbolicLink("")
	assert.False(isSymbolicLink)

	isSymbolicLink = manager.IsSymbolicLink(tmpFile.Name())
	assert.False(isSymbolicLink)

	// Create a symbolic link to the file.
	symlinkPath := path.Join(tmpDir, "symlink")
	err = os.Symlink(tmpFile.Name(), symlinkPath)
	if assert.NoError(err) {
		defer Remove(symlinkPath)
	}

	isSymbolicLink = manager.IsSymbolicLink(symlinkPath)
	assert.True(isSymbolicLink)
}

func TestUnixManager_CreateDirectory(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	newDir := path.Join(tmpDir, "new-dir")
	err := manager.CreateDirectory(newDir, false)
	if assert.NoError(err) {
		defer Remove(newDir)
	}
	assert.DirExists(newDir)

	missingParent := path.Join(tmpDir, "missing-parent", "new-dir")
	err = manager.CreateDirectory(missingParent, false)
	assert.Error(err)
	assert.NoDirExists(missingParent)

	// Assert recursive directory creation.
	err = manager.CreateDirectory(missingParent, true)
	if assert.NoError(err) {
		defer Remove(path.Join(tmpDir, "missing-parent"))
	}
	assert.DirExists(missingParent)
}

func TestUnixManager_CopyFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	originalFile, _, originalHash := createTestFile(t, 1024)
	overwriteFile, _, overwriteHash := createTestFile(t, 1024)

	assert.NotEqual(originalHash, overwriteHash)

	defer Remove(originalFile.Name())
	defer Remove(overwriteFile.Name())

	copiedFile := path.Join(tmpDir, "copied-file")
	err := manager.CopyFile(originalFile.Name(), copiedFile, false)
	if assert.NoError(err) {
		defer Remove(copiedFile)
	}
	assert.FileExists(copiedFile)
	assertFileHash(t, copiedFile, originalHash)

	// Copy the file again, but this time overwrite the existing file.
	err = manager.CopyFile(overwriteFile.Name(), copiedFile, true)
	assert.NoError(err)
	assertFileHash(t, copiedFile, overwriteHash)
}

func TestUnixManager_CreateHardLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	originalFile, _, originalHash := createTestFile(t, 1024)
	overwriteFile, _, overwriteHash := createTestFile(t, 1024)

	assert.NotEqual(originalHash, overwriteHash)

	defer Remove(originalFile.Name())
	defer Remove(overwriteFile.Name())

	hardLinkPath := path.Join(tmpDir, "hard-link")
	err := manager.CreateHardLink(originalFile.Name(), hardLinkPath, false)
	if assert.NoError(err) {
		defer Remove(hardLinkPath)
	}
	assert.FileExists(hardLinkPath)
	assertFileHash(t, hardLinkPath, originalHash)

	err = manager.CreateHardLink(overwriteFile.Name(), hardLinkPath, true)
	assert.NoError(err)
	assertFileHash(t, hardLinkPath, overwriteHash)
}

func TestUnixManager_CreateSymbolicLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)
	tmpDir := t.TempDir()

	originalFile, _, originalHash := createTestFile(t, 1024)
	overwriteFile, _, overwriteHash := createTestFile(t, 1024)

	assert.NotEqual(originalHash, overwriteHash)

	defer Remove(originalFile.Name())
	defer Remove(overwriteFile.Name())

	symlinkPath := path.Join(tmpDir, "symlink")
	err := manager.CreateSymbolicLink(originalFile.Name(), symlinkPath, false)
	if assert.NoError(err) {
		defer Remove(symlinkPath)
	}
	assert.FileExists(symlinkPath)
	assertFileHash(t, symlinkPath, originalHash)

	err = manager.CreateSymbolicLink(overwriteFile.Name(), symlinkPath, true)
	assert.NoError(err)

	assert.FileExists(symlinkPath)
	assertFileHash(t, symlinkPath, overwriteHash)
}

func TestUnixManager_ReadOwnerFromFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	currenUser, err := user.Current()
	assertions.NoError(t, err)

	tmpDir := t.TempDir()
	tmpFile, err := os.MkdirTemp(tmpDir, "read-owner")
	assertions.NoError(t, err)
	defer RemoveAll(tmpDir)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	pm := setupMockPrincipalManager(t, ctrl)
	assert, manager := setupTest(t, pm)

	usr, grp, err := manager.ReadOwner(tmpFile)
	if assert.NoError(err) {
		assert.Equal(currenUser.Name, usr.Name())
		assert.Equal(currenUser.Gid, grp.Gid())
	}
}

func TestUnixManager_ReadOwnerFromDirectory(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	currenUser, err := user.Current()
	assertions.NoError(t, err)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	pm := setupMockPrincipalManager(t, ctrl)
	assert, manager := setupTest(t, pm)

	owner, grp, err := manager.ReadOwner(t.TempDir())
	if assert.NoError(err) {
		assert.Equal(currenUser.Name, owner.Name())
		assert.Equal(currenUser.Gid, grp.Gid())
	}
}

func TestUnixManager_ReadPermsFromFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	perms, err := manager.ReadPermissions("/etc/passwd")
	if assert.NoError(err) {
		assert.Equal(chmodPermNotationToFileMode("644"), perms)
	}
}

func TestUnixManager_ReadPermsFromDirectory(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	perms, err := manager.ReadPermissions("/etc")
	if assert.NoError(err) {
		assert.Equal(chmodPermNotationToFileMode("755"), perms)
	}
}

func TestUnixManager_ReadPermsFileNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	_, err := manager.ReadPermissions("/xyz")
	assert.Error(err)
}

func TestUnixManager_ReadOwnerFileNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	_, _, err := manager.ReadOwner("/xyz")
	assert.Error(err)
}

func TestUnixManager_WriteOwner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	// Create a temporary file to use as the test path
	tempFile, err := os.CreateTemp("", "testfile")
	assert.NoError(err)
	defer Remove(tempFile.Name())

	err = os.Chmod(tempFile.Name(), chmodPermNotationToFileMode("777"))
	assert.NoError(err)

	u, err := user.Current()
	assert.NoError(err)

	g, err := user.LookupGroupId(u.Gid)
	assert.NoError(err)

	usr, err := pm.LookupUserByName(u.Username)
	assert.NoError(err)

	grp, err := pm.LookupGroupById(g.Gid)
	assert.NoError(err)

	// Write the owner to the temporary file
	err = manager.WriteOwner(tempFile.Name(), usr, grp, false)
	if assert.NoError(err) {
		// Check that the file owner is correct
		fileInfo, err := os.Stat(tempFile.Name())
		if assert.NoError(err) {
			assert.Equal(usr.Uid(), strconv.FormatUint(uint64(fileInfo.Sys().(*syscall.Stat_t).Uid), 10))
			assert.Equal(grp.Gid(), strconv.FormatUint(uint64(fileInfo.Sys().(*syscall.Stat_t).Gid), 10))
		}
	}
}

func TestUnixManager_WriteOwnerBadUid(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	usr := principal.NewMockUser(ctrl)
	usr.EXPECT().Uid().Return("not_a_number")
	usr.EXPECT().Uid().Return("not_a_number")

	err := manager.WriteOwner("not_a_path", usr, nil, false)
	assert.Error(err)
}

func TestUnixManager_WriteOwnerBadGid(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	usr := principal.NewMockUser(ctrl)
	usr.EXPECT().Uid().Return("1234")
	grp := principal.NewMockGroup(ctrl)
	grp.EXPECT().Gid().Return("not_a_number")
	grp.EXPECT().Gid().Return("not_a_number")

	err := manager.WriteOwner("not_a_path", usr, grp, false)
	assert.Error(err)
}

func TestUnixManager_WriteOwnerToFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	usr := principal.NewMockUser(ctrl)
	usr.EXPECT().Uid().Return("1234").AnyTimes()
	usr.EXPECT().Name().Return("user1").AnyTimes()
	grp := principal.NewMockGroup(ctrl)
	grp.EXPECT().Gid().Return("1234").AnyTimes()
	grp.EXPECT().Name().Return("user-group").AnyTimes()

	err := manager.WriteOwner("not_a_path", usr, grp, false)
	assert.Error(err)
}

func TestUnixManager_WritePerms(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	tempFile, err := os.CreateTemp("", "testfile")
	assert.NoError(err)
	defer Remove(tempFile.Name())

	originalPerms, err := manager.ReadPermissions(tempFile.Name())
	rwxAllPerms := chmodPermNotationToFileMode("777")
	assert.NotEqual(rwxAllPerms, originalPerms)

	err = manager.WritePermissions(tempFile.Name(), rwxAllPerms, false)
	newPerms, err := manager.ReadPermissions(tempFile.Name())
	assert.Equal(rwxAllPerms, newPerms)
	assert.NoError(err)
}

func TestUnixManager_WritePermsWithBadPerms(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	err := manager.WritePermissions("not_a_file", chmodPermNotationToFileMode("777"), false)
	assert.Error(err)
}

func TestUnixManager_WritePermsRecursively(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	tempDir, err := os.MkdirTemp("", "testdir2")
	assert.NoError(err)
	fmt.Println(tempDir)

	tempFile1, err := os.CreateTemp(tempDir, "testfile1")
	assert.NoError(err)
	fmt.Println(tempFile1.Name())

	tempFile2, err := os.CreateTemp(tempDir, "testfile2")
	assert.NoError(err)
	fmt.Println(tempFile2.Name())

	rwxAllPerms := chmodPermNotationToFileMode("777")
	err = manager.WritePermissions(tempDir, rwxAllPerms, true)
	assert.NoError(err)

	perms, err := manager.ReadPermissions(tempDir)
	assert.NoError(err)
	assert.Equal(rwxAllPerms, perms)

	perms, err = manager.ReadPermissions(tempFile1.Name())
	assert.NoError(err)
	assert.Equal(rwxAllPerms, perms)

	perms, err = manager.ReadPermissions(tempFile2.Name())
	assert.NoError(err)
	assert.Equal(rwxAllPerms, perms)
}

func TestUnixManager_WritePermsRecursivelyWithBadPerms(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	err := manager.WritePermissions("not_a_file", chmodPermNotationToFileMode("777"), true)
	assert.Error(err)
}

func TestUnixManager_WriteOwnerRecursively(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	tempDir, err := os.MkdirTemp("", "testdir")
	assert.NoError(err)
	fmt.Println(tempDir)

	tempFile1, err := os.CreateTemp(tempDir, "testfile1")
	assert.NoError(err)
	fmt.Println(tempFile1.Name())

	tempFile2, err := os.CreateTemp(tempDir, "testfile2")
	assert.NoError(err)
	fmt.Println(tempFile2.Name())

	rwxAllPerms := chmodPermNotationToFileMode("777")
	tempDir2 := path.Join(tempDir, "testdir2")
	err = os.Mkdir(tempDir2, rwxAllPerms)
	assert.NoError(err)
	fmt.Println(tempDir2)

	tempFile3, err := os.CreateTemp(tempDir2, "testfile3")
	assert.NoError(err)
	fmt.Println(tempFile3.Name())

	currentUser, err := user.Current()
	assert.NoError(err)

	usr, err := pm.LookupUserByName(currentUser.Username)
	assert.NoError(err)

	grp, err := pm.LookupGroupById(usr.PrimaryGroup().Gid())
	assert.NoError(err)

	err = manager.WriteOwner(tempDir, usr, grp, true)
	assert.NoError(err)

	assertFileOwnership(t, manager, tempDir, usr, grp)
	assertFileOwnership(t, manager, tempFile1.Name(), usr, grp)
	assertFileOwnership(t, manager, tempFile2.Name(), usr, grp)
	assertFileOwnership(t, manager, tempDir2, usr, grp)
	assertFileOwnership(t, manager, tempFile3.Name(), usr, grp)
}

func TestUnixManager_WriteOwnerRecursivelyBadUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	usr := principal.NewMockUser(ctrl)
	usr.EXPECT().Uid().Return("not_a_number")
	usr.EXPECT().Uid().Return("not_a_number")

	err := manager.WriteOwner("not_a_path", usr, nil, true)
	assert.Error(err)
}

func TestUnixManager_WriteOwnerRecursivelyBadGroup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	usr := principal.NewMockUser(ctrl)
	usr.EXPECT().Uid().Return("1234")
	grp := principal.NewMockGroup(ctrl)
	grp.EXPECT().Gid().Return("not_a_number")
	grp.EXPECT().Gid().Return("not_a_number")

	err := manager.WriteOwner("not_a_path", usr, grp, true)
	assert.Error(err)
}

func TestUnixManager_ReadFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManagerForHedera(t, ctrl)

	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert, manager := setupTest(t, pm)

	tmpFile := filepath.Join(t.TempDir(), "test")
	payload := []byte("test")
	err := manager.WriteFile(tmpFile, payload)
	assert.NoError(err)

	fileInfo, exists, err := manager.PathExists(tmpFile)
	assert.NoError(err)
	assert.True(exists)
	assert.Equal(security.ACLFilePerms, fileInfo.Mode().Perm())

	_, err = manager.ReadFile(tmpFile, 1)
	assert.Error(err)

	b, err := manager.ReadFile(tmpFile, int64(len(payload)))
	assert.NoError(err)
	assert.Equal(payload, b)

	b, err = manager.ReadFile(tmpFile, -1) // disable file size check
	assert.NoError(err)
	assert.Equal(payload, b)
}

func TestUnixManager_ReadFile_Failures(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := setupMockPrincipalManagerForHedera(t, ctrl)
	assert, manager := setupTest(t, pm)
	tmpFile := filepath.Join(t.TempDir(), "test")
	payload := []byte("test")

	// fail to read non-existing file
	_, err := manager.ReadFile(tmpFile, int64(len(payload)))
	assert.Error(err)
	assert.True(errorx.IsOfType(err, FileNotFound))

	// write the file
	err = manager.WriteFile(tmpFile, payload)
	assert.NoError(err)

	// fail for file size larger than 1
	_, err = manager.ReadFile(tmpFile, 1)
	assert.Error(err)

	// change permission so that it cannot be read
	currentUser, err := user.Current()
	assert.NoError(err)
	if currentUser.Uid != "0" { // we can only test permission issue if the test is not run using root usr
		err = manager.WritePermissions(tmpFile, 0333, false)
		require.NoError(t, err)

		_, err = manager.ReadFile(tmpFile, int64(len(payload)))
		assert.Error(err)

		// succeed with correct permission
		err = manager.WritePermissions(tmpFile, security.ACLFilePerms, false)
		require.NoError(t, err)
		b, err := manager.ReadFile(tmpFile, int64(len(payload)))
		assert.NoError(err)
		assert.Equal(payload, b)
	}

	// write a 0 length file
	err = manager.WriteFile(tmpFile, []byte{})
	b, err := manager.ReadFile(tmpFile, int64(len(payload)))
	assert.NoError(err)
	assert.Equal(0, len(b))
}

func TestUnixManager_WriteFile_Failures(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := principal.NewMockManager(ctrl)
	assert, manager := setupTest(t, pm)
	tmpFile := filepath.Join(t.TempDir(), "test")
	payload := []byte("test")

	// fail to write to an invalid path
	invalidDir := filepath.Join(filepath.Dir(t.TempDir()), "/INVALID/test")
	err := manager.WriteFile(invalidDir, payload)
	assert.Error(err)

	// fail on getting usr
	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(nil, errorx.IllegalState.New("mock error"))
	assert, manager = setupTest(t, pm)
	err = manager.WriteFile(tmpFile, payload)
	assert.Error(err)

	// fail on getting grp
	usr := principal.NewMockUser(ctrl)
	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(usr, nil)
	pm.EXPECT().LookupGroupByName(security.ServiceAccountUserName).Return(nil, errorx.IllegalState.New("mock error"))
	assert, manager = setupTest(t, pm)
	err = manager.WriteFile(tmpFile, payload)
	assert.Error(err)
}

// This integration test requires to be run using sudo and for hedera usr to exist.
//
// # It is designed to be run from simulated node container
//
// Steps to run:
// - cd .github/workflows/support/docker/simulated-node
// - Run ./githubstandup.sh.
// - After running ./standup.sh
//   - cd /source/src/go-tools
//   - make setup
//   - sudo env PATH="$PATH" go test -v golang.hedera.com/solo-provisioner/pkg/fsx -run TestUnixManager_WriteOwnerRecursivelyFromRoot_IT
func TestUnixManager_WriteOwnerRecursivelyFromRoot_AsRoot(t *testing.T) {
	currentUser, err := user.Current()
	assertions.NoError(t, err)
	if currentUser.Uid != "0" {
		t.Skipf("skipping test that requires root usr, found: %s", currentUser)
	}

	pm, err := principal.NewManager()
	assertions.NoError(t, err)

	assert, manager := setupTest(t, pm)

	// check for root ownership of a /etc/passwd
	etcOwner, etcOwnerGroup, err := manager.ReadOwner("/etc/passwd")
	if assert.NoError(err) {
		assert.Equal("root", etcOwner.Name())

		if runtime.GOOS == "darwin" {
			assert.Equal("wheel", etcOwnerGroup.Name())
		} else {
			assert.Equal("root", etcOwnerGroup.Name())
		}
		assert.Equal(currentUser.Name, etcOwner.Name())
		assert.Equal(currentUser.Gid, etcOwnerGroup.Gid())
	}

	// check for root ownership of /etc directory
	etcOwner, etcOwnerGroup, err = manager.ReadOwner("/etc")
	if assert.NoError(err) {
		assert.Equal("root", etcOwner.Name())

		if runtime.GOOS == "darwin" {
			assert.Equal("wheel", etcOwnerGroup.Name())
		} else {
			assert.Equal("root", etcOwnerGroup.Name())
		}
		assert.Equal(currentUser.Name, etcOwner.Name())
		assert.Equal(currentUser.Gid, etcOwnerGroup.Gid())
	}

	rootUser, err := pm.LookupUserByName(currentUser.Username)
	assert.NoError(err)
	rootGroup, err := pm.LookupGroupById(rootUser.PrimaryGroup().Gid())
	assert.NoError(err)

	usr, err := pm.LookupUserByName("hedera")
	if err != nil {
		t.Skip("skipping test that requires hedera usr")
	}
	grp, err := pm.LookupGroupById(usr.PrimaryGroup().Gid())
	assert.NoError(err)

	tempDir, err := os.MkdirTemp("", "testdir")
	assert.NoError(err)
	fmt.Println(tempDir)

	tempFile1, err := os.CreateTemp(tempDir, "testfile1")
	assert.NoError(err)
	fmt.Println(tempFile1.Name())

	tempFile2, err := os.CreateTemp(tempDir, "testfile2")
	assert.NoError(err)
	fmt.Println(tempFile2.Name())

	rwxAllPerms := chmodPermNotationToFileMode("777")
	tempDir2 := path.Join(tempDir, "testdir2")
	err = os.Mkdir(tempDir2, rwxAllPerms)
	assert.NoError(err)
	fmt.Println(tempDir2)

	tempFile3, err := os.CreateTemp(tempDir2, "testfile3")
	assert.NoError(err)
	fmt.Println(tempFile3.Name())

	assertFileOwnership(t, manager, tempDir, rootUser, rootGroup)
	assertFileOwnership(t, manager, tempFile1.Name(), rootUser, rootGroup)
	assertFileOwnership(t, manager, tempFile2.Name(), rootUser, rootGroup)
	assertFileOwnership(t, manager, tempDir2, rootUser, rootGroup)
	assertFileOwnership(t, manager, tempFile3.Name(), rootUser, rootGroup)

	err = manager.WriteOwner(tempDir, usr, grp, true)
	assert.NoError(err)

	assertFileOwnership(t, manager, tempDir, usr, grp)
	assertFileOwnership(t, manager, tempFile1.Name(), usr, grp)
	assertFileOwnership(t, manager, tempFile2.Name(), usr, grp)
	assertFileOwnership(t, manager, tempDir2, usr, grp)
	assertFileOwnership(t, manager, tempFile3.Name(), usr, grp)
}

func createTestFile(t *testing.T, size uint) (*os.File, []byte, [32]byte) {
	t.Helper()
	req := require.New(t)

	data := make([]byte, size)
	_, err := rand.Read(data)
	req.NoError(err)

	hash := sha256.Sum256(data)

	tmpFile, err := os.CreateTemp(t.TempDir(), "test-file.*")
	req.NoError(err)

	req.NoError(os.WriteFile(tmpFile.Name(), data, 0644))
	req.NoError(tmpFile.Sync())

	return tmpFile, data, hash
}

func assertFileHash(t *testing.T, path string, expectedHash [32]byte) {
	t.Helper()
	req := require.New(t)

	contents, err := os.ReadFile(path)
	req.NoError(err)

	actualHash := sha256.Sum256(contents)

	req.Equal(expectedHash, actualHash)
}
