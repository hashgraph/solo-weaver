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
	"github.com/cockroachdb/errors"
	"github.com/golang/mock/gomock"
	"github.com/mitchellh/go-ps"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPlockManager_New(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	_, err = NewLockManager(tmpDir)
	req.NoError(err)

	_, err = NewLockManager("INVALID_DIR")
	req.Error(err)

	_, err = newLockManagerWithStore(nil)
	req.EqualError(err, "The argument 'store' with a value of '<nil>' is invalid: 'file store cannot be nil'")
}

func TestPlockManager_Discover(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	maxCount := -1
	err = manager.SetWorkDir("INVALID_DIR")
	//req.EqualError(err, "The argument 'dirName' with a value of 'INVALID_DIR' is invalid: 'invalid work directory'")

	helper := testHelper{}
	mockInfo := helper.createStaleLock(t, tmpDir)

	manager.SetWorkDir(mockInfo.WorkDir)
	locks, err := manager.Discover(maxCount)
	req.NoError(err)
	req.Equal(1, len(locks))

	info, ok := locks[mockInfo.PID]
	req.True(ok)
	req.Equal(mockInfo.LockFilePath, info.LockFilePath)

	// new locks
	lockFile2 := plockFileName("test")
	tmpPidFile2 := plockPidFilename("test", mockInfo.PID+1)
	_, err = store.Create(tmpPidFile2)
	req.NoError(err)
	_, err = store.Link(tmpPidFile2, lockFile2)
	req.NoError(err)
	locks, err = manager.Discover(maxCount)
	req.NoError(err)
	req.Equal(2, len(locks))

	// use mock to trigger filesystem failures
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	storeMock := NewMockfileStore(ctrl)
	manager, err = newLockManagerWithStore(storeMock)
	req.NoError(err)
	storeMock.EXPECT().GetWorkDir().Times(2).Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, LockFileExtension, -1).Return(nil, errors.New("test"))
	_, err = manager.DiscoverStaleLocks(maxCount)
	req.Error(err)
}

func TestPlockManager_DiscoverStaleLocks(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	helper := testHelper{}
	mockInfo := helper.createStaleLock(t, tmpDir)

	// create lock with correct pid
	pid := os.Getpid()
	lockFile2 := plockFilePath("test", mockInfo.WorkDir)
	tmpPidFile2 := plockPidFilePath("test", mockInfo.WorkDir, pid)
	os.Create(tmpPidFile2)
	os.Link(tmpPidFile2, lockFile2)

	maxCount := -1
	locks, err := manager.DiscoverStaleLocks(maxCount)
	req.NoError(err)
	req.Equal(1, len(locks))

	lock, ok := locks[mockInfo.PID]
	req.True(ok)
	req.Equal(mockInfo.LockFilePath, lock.LockFilePath)

	// removing stale lock should return 0 locks
	os.Remove(mockInfo.LockFilePath)
	os.Remove(mockInfo.PidFilePath)
	locks, err = manager.DiscoverStaleLocks(maxCount)
	req.NoError(err)
	req.Equal(0, len(locks))

	// use mock to trigger filesystem failures
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	storeMock := NewMockfileStore(ctrl)
	manager, err = newLockManagerWithStore(storeMock)
	req.NoError(err)
	storeMock.EXPECT().GetWorkDir().Times(2).Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, LockFileExtension, -1).Return(nil, errors.New("test"))
	_, err = manager.DiscoverStaleLocks(maxCount)
	req.Error(err)
}

func TestPlockManager_DiscoverByPID(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	helper := testHelper{}
	mockInfo := helper.createStaleLock(t, tmpDir)

	locks, err := manager.DiscoverByPID(mockInfo.PID)
	req.NoError(err)
	req.Equal(1, len(locks))

	// removing lock file should return 0 lock
	os.Remove(mockInfo.LockFilePath)
	locks, err = manager.DiscoverByPID(mockInfo.PID)
	req.NoError(err)
	req.Equal(0, len(locks))

	// use mock to trigger filesystem failures
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	storeMock := NewMockfileStore(ctrl)
	manager, err = newLockManagerWithStore(storeMock)
	req.NoError(err)
	storeMock.EXPECT().GetWorkDir().Times(2).Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, plockPidFileSuffix(mockInfo.PID), -1).Return(nil, errors.New("test"))
	_, err = manager.DiscoverByPID(mockInfo.PID)
	req.Error(err)
}

func TestPlockManager_DiscoverByLockName(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	err = manager.SetWorkDir("INVALID_DIR")
	req.EqualError(err, "The argument 'dirName' with a value of 'INVALID_DIR' is invalid: 'invalid work directory'")

	helper := testHelper{}
	mockInfo := helper.createStaleLock(t, tmpDir)

	lock1, err := manager.DiscoverByLockName(mockInfo.Name)
	req.NoError(err)
	req.Equal(mockInfo.PID, lock1.PID)
}

func TestPlockManager_ResetStaleLock(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	helper := testHelper{}
	mockInfo := helper.createStaleLock(t, tmpDir)

	err = manager.ResetStaleLock(*mockInfo)
	req.NoError(err)
	req.False(fileExists(mockInfo.LockFilePath))
	req.False(fileExists(mockInfo.PidFilePath))

	mockPID := mockInfo.PID
	mockInfo.PID = os.Getpid() // make a valid pid, which should throw illegal argument exception
	err = manager.ResetStaleLock(*mockInfo)
	req.Error(err)
	mockInfo.PID = mockPID

	// use mock to trigger filesystem failures
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	storeMock := NewMockfileStore(ctrl)
	manager, err = newLockManagerWithStore(storeMock)
	req.NoError(err)

	// if pid file cannot be deleted, it should return error
	storeMock.EXPECT().Exists(plockPidFilename(mockInfo.Name, mockInfo.PID)).Return(true)
	storeMock.EXPECT().Delete(plockPidFilename(mockInfo.Name, mockInfo.PID)).Return(errors.New("mock error"))
	err = manager.ResetStaleLock(*mockInfo)
	req.Error(err)

	// if lock file cannot be deleted, it should return error
	storeMock.EXPECT().Exists(mockInfo.PidFileName).Return(true)
	storeMock.EXPECT().Delete(mockInfo.PidFileName).Return(nil)
	storeMock.EXPECT().Exists(mockInfo.LockFileName).Return(true)
	storeMock.EXPECT().Delete(mockInfo.LockFileName).Return(errors.New("mock error"))
	err = manager.ResetStaleLock(*mockInfo)
	req.Error(err)
}

func TestPlockManager_ResetStaleLocks(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	helper := testHelper{}
	mockInfo1 := helper.createTestLock(t, 200000, tmpDir)
	mockInfo2 := helper.createTestLock(t, 300000, tmpDir)

	err = manager.ResetStaleLocks()
	req.NoError(err)
	req.False(fileExists(mockInfo1.LockFilePath))
	req.False(fileExists(mockInfo1.PidFilePath))
	req.False(fileExists(mockInfo2.LockFilePath))
	req.False(fileExists(mockInfo2.PidFilePath))

	// discover locks should fail
	manager.SetWorkDir("INVALID_WORK_DIR")
	err = manager.ResetStaleLocks()
	req.NoError(err)

	// use mock to trigger filesystem failures
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	storeMock := NewMockfileStore(ctrl)
	manager, err = newLockManagerWithStore(storeMock)
	req.NoError(err)

	mockInfo := helper.createTestLock(t, 20000, tmpDir)

	// if file list errors, it should fail
	storeMock.EXPECT().GetWorkDir().Times(2).Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, LockFileExtension, -1).Return(nil, errors.New("test"))
	err = manager.ResetStaleLocks()
	req.Error(err)

	// create file list to mock List function call
	lockFileInfo, _ := os.Stat(mockInfo.LockFilePath)
	pidFileInfo, _ := os.Stat(mockInfo.PidFilePath)
	fileList := []os.FileInfo{
		lockFileInfo,
		pidFileInfo,
	}

	// prepare store mock and return error when it tries to delete the pid file
	storeMock.EXPECT().GetWorkDir().AnyTimes().Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, LockFileExtension, -1).Return(fileList, nil)
	storeMock.EXPECT().FullPath(mockInfo.LockFileName).Return(mockInfo.LockFilePath)
	storeMock.EXPECT().FullPath(mockInfo.PidFileName).Return(mockInfo.PidFileName)
	storeMock.EXPECT().Stat(mockInfo.LockFileName).Return(lockFileInfo, nil)
	storeMock.EXPECT().ProviderType().Return(ProviderLocal)
	storeMock.EXPECT().Exists(mockInfo.PidFileName).Return(true)
	storeMock.EXPECT().Delete(mockInfo.PidFileName).Return(errors.New("mock error"))
	err = manager.ResetStaleLocks()
	req.Error(err)
}

func TestPlockManager_ResetLock_Failure_Cases(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	helper := testHelper{}
	mockInfo1 := helper.createTestLock(t, 200000, tmpDir)

	killable := []string{"INVALID"}
	err = manager.ResetLock(mockInfo1.PID, killable)
	req.NoError(err)
	req.False(fileExists(mockInfo1.LockFilePath))
	req.False(fileExists(mockInfo1.PidFilePath))

	mockInfo2 := helper.createTestLock(t, os.Getpid(), tmpDir) // valid pid, this shouldn't be reset
	err = manager.ResetLock(mockInfo2.PID, killable)
	req.Error(err)
	req.True(fileExists(mockInfo2.LockFilePath))
	req.True(fileExists(mockInfo2.PidFilePath))

	// discover locks should fail
	mockInfo3 := helper.createTestLock(t, 200000, tmpDir)
	manager.SetWorkDir("INVALID_DIR")
	err = manager.ResetLock(mockInfo3.PID, killable)
	req.NoError(err)

	// use mock to trigger filesystem failures
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	storeMock := NewMockfileStore(ctrl)
	manager, err = newLockManagerWithStore(storeMock)
	req.NoError(err)

	mockInfo := helper.createTestLock(t, 20000, tmpDir)

	// if file list errors, it should fail
	killable = []string{} // empty array, we don't want anything to be killed during failure case tests
	storeMock.EXPECT().GetWorkDir().AnyTimes().Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, plockPidFileSuffix(mockInfo.PID), -1).Return(nil, errors.New("test"))
	err = manager.ResetLock(mockInfo.PID, killable)
	req.Error(err)

	// create file list to mock List function call
	lockFileInfo, _ := os.Stat(mockInfo.LockFilePath)
	pidFileInfo, _ := os.Stat(mockInfo.PidFilePath)
	fileList := []os.FileInfo{
		lockFileInfo,
		pidFileInfo,
	}

	// prepare store mock and return error when it tries to delete the pid file
	storeMock.EXPECT().GetWorkDir().AnyTimes().Return(tmpDir)
	storeMock.EXPECT().List(tmpDir, plockPidFileSuffix(mockInfo.PID), -1).Return(fileList, nil)
	storeMock.EXPECT().FullPath(mockInfo.LockFileName).Return(mockInfo.LockFilePath)
	storeMock.EXPECT().FullPath(mockInfo.PidFileName).Return(mockInfo.PidFileName)
	storeMock.EXPECT().Stat(mockInfo.LockFileName).Return(lockFileInfo, nil)
	storeMock.EXPECT().ProviderType().Return(ProviderLocal)
	storeMock.EXPECT().Exists(mockInfo.PidFileName).Return(true)
	storeMock.EXPECT().Delete(mockInfo.PidFileName).Return(errors.New("mock error"))
	err = manager.ResetLock(mockInfo.PID, killable)
	req.Error(err)
}

func TestPlockManager_Rest_Kill_PID(t *testing.T) {
	req := require.New(t)
	// spawn a child process that we shall try to Reset
	cmd := exec.Command("sleep", "1000")
	err := cmd.Start()
	defer func() {
		cmd.Process.Kill()
	}()

	req.NoError(err)
	req.NoError(cmd.Err)
	req.NotNil(cmd.Process)

	// setup tmpDir
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager, err := newLockManagerWithStore(store)
	req.NoError(err)

	// acquire a lock
	pid := cmd.Process.Pid
	plock, perr := NewLock("plock", tmpDir, pid)
	req.NoError(perr)
	perr = plock.Acquire()
	req.NoError(perr)
	defer plock.Release() // defer Release

	process, err := ps.FindProcess(pid)
	req.Equal(os.Getpid(), process.PPid())
	req.NoError(err)
	req.NotNil(process)

	err = manager.ResetLock(pid, []string{process.Executable()})
	req.NoError(err)

	cmd.Wait() // force it to wait so that we can check if the PID is stale in the next line
	req.True(isStalePID(process.Pid()))
	req.True(plock.IsAcquired()) // it should still say we got the lock, although it has been reset without calling plock.Release()
}
func TestPlockManager_GetFileModifiedAt(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test*")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)
	tmpFile, err := os.CreateTemp(tmpDir, "plock-util")
	req.NoError(err)
	defer tmpFile.Close()

	filePath := tmpFile.Name()
	fileInfo, err := os.Stat(filePath)
	req.NoError(err)
	modifiedAtExp := fileInfo.ModTime()

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager := &plockManager{
		store: store,
	}

	modifiedAt, err := manager.getFileModifiedAt(fileInfo.Name())
	req.NoError(err)
	req.Equal(modifiedAtExp.Format(time.RFC3339), modifiedAt.Format(time.RFC3339))

}

func TestPlockManager_PidFileToLockInfo(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test*")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	lockFile, err := os.Create(plockFilePath("test", tmpDir))
	req.NoError(err)
	defer lockFile.Close()

	pidFile, err := os.Create(plockPidFilePath("test", tmpDir, 2000))
	req.NoError(err)
	defer pidFile.Close()

	store, err := newLocalFileStore(tmpDir)
	req.NoError(err)
	manager := &plockManager{
		store: store,
	}
	info, err := manager.pidFileToLockInfo(pidFile.Name())
	req.NoError(err)
	req.NotNil(info)
	req.Equal(2000, info.PID)
	req.Equal(lockFile.Name(), info.LockFilePath)
	req.Equal(pidFile.Name(), info.PidFilePath)

	pidFile2, err := os.Create(plockPidFilePath("test", tmpDir, -2000)) // negative PID should error
	req.NoError(err)
	defer pidFile2.Close()
	_, err = manager.pidFileToLockInfo(pidFile2.Name())
	req.Error(err)

	pidFile3, err := os.Create(plockPidFilePath("test", tmpDir, -20-00)) // invalid PID
	req.NoError(err)
	defer pidFile3.Close()
	_, err = manager.pidFileToLockInfo(pidFile2.Name())
	req.Error(err)
}
