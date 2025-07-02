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
	"context"
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"os"
	"os/user"
	"sync"
	"testing"
	"time"
)

func TestPlockLocal_NewPlock(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	// success
	pid := info.PID
	_, perr := NewLock(lockName, workDir, pid)

	// *** failure scenario ****

	// fail to initialize when directory doesn't exist
	_, perr = NewLock(lockName, ".INVALID", pid)
	req.Error(perr)

	// fail to initialize when directory doesn't have proper permissions
	os.Chmod(workDir, os.ModeDir)
	_, perr = NewLock(lockName, workDir, pid)
	req.Error(perr)
}

func TestPlockLocal_PrepareLock(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	store, err := newLocalFileStore(workDir)
	req.NoError(err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewMockLockManager(ctrl)

	// invalid format error
	_, perr := prepareLock("$%^&*", info.PID, store, manager)
	req.Error(perr)

	// file IO error
	_, perr = prepareLock(lockName, info.PID, nil, manager)
	req.Error(perr)

	// file IO error
	_, perr = prepareLock(lockName, info.PID, store, nil)
	req.Error(perr)

	lock, perr := prepareLock(lockName, -1, store, manager)
	req.NoError(perr)
	req.Equal(info.PID, lock.Info().PID)
	req.False(lock.IsAcquired())
}

func TestPlockLocal_Fail_Acquire_If_Directory_Permission_Changes(t *testing.T) {
	req := require.New(t)

	currentUser, err := user.Current()
	req.NoError(err)
	if currentUser.Uid == "0" { // we can only test permission issue if the test is not run using root user
		t.Skipf("cannot run permission related test as root user (%s)", currentUser)
		return
	}

	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	pid := info.PID
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())

	// fail to Acquire if directory permission changes after initialization
	stat, _ := os.Stat(workDir)
	t.Log(workDir, stat.Mode())
	os.Chmod(workDir, os.ModeDir)
	stat, _ = os.Stat(workDir)
	t.Log(workDir, stat.Mode())
	perr = plock.Acquire()
	req.Error(perr)
}

func TestPlockLocal_Fail_Acquire_Same_Lock(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	// acquire a lock
	pid := info.PID
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())
	perr = plock.Acquire()
	req.NoError(perr)

	// cannot re-acquire without releasing the existing lock
	perr = plock.Acquire()
	req.Error(perr)

	// trying to require with same lock name with different PID would throw lock exists error
	pid2 := pid + 1
	plock2, perr := NewLock(lockName, workDir, pid2)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())
	perr = plock2.Acquire()
	req.Error(perr)

	// create a new lock with different name but same pid should succeed
	lockName2 := fmt.Sprintf("%s-2", lockName)
	plock3, perr := NewLock(lockName2, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock3.Info())
	perr = plock3.Acquire()
	req.NoError(perr)

}

func TestPlockLocal_Acquire_Commit_Phase_Failure(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	store := NewMockfileStore(ctrl)
	lockFileName := plockFileName(lockName)
	lockFilePath := plockFilePath(lockName, workDir)
	lockPidFileName := plockPidFilename(lockName, info.PID)
	lockPidFilePath := plockPidFilePath(lockName, workDir, info.PID)
	store.EXPECT().Create(lockPidFileName).Return(nil, nil)
	store.EXPECT().ProviderType().Return(ProviderLocal)
	store.EXPECT().
		FullPath(lockFileName).Times(2).
		Return(lockFilePath)
	store.EXPECT().
		FullPath(lockPidFileName).
		Return(lockPidFilePath)
	store.EXPECT().
		GetWorkDir().
		Return(workDir)
	store.EXPECT().
		Exists(lockPidFileName).Return(true)
	store.EXPECT().
		Delete(lockPidFileName).Return(nil)

	manager := NewMockLockManager(ctrl)
	manager.EXPECT().DiscoverByLockName(lockName).Return(nil, nil)
	plock4, perr := prepareLock(lockName, info.PID, store, manager)
	req.NoError(perr)
	req.NotEqual(info, plock4.Info())

	// linking error should error on acquire
	store.EXPECT().
		Link(plockPidFilename(lockName, info.PID), lockFileName).
		Return(nil, errors.New("Unexpected OS error"))
	perr = plock4.Acquire()
	req.Error(perr)
}

func TestPlockLocal_Acquire_Verify_Phase_Failure(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	store := NewMockfileStore(ctrl)
	lockFileName := plockFileName(lockName)
	lockFilePath := plockFilePath(lockName, workDir)
	pidFileName := plockPidFilename(lockName, info.PID)
	pidFilePath := plockPidFilePath(lockName, workDir, info.PID)

	// create pidFile
	pidFile, err := os.Create(pidFilePath)
	req.NoError(err)
	pidFileInfo, err := pidFile.Stat()
	req.NoError(err)

	// mock store APIs
	store.EXPECT().ProviderType().Return(ProviderLocal)
	store.EXPECT().Create(pidFileName).Return(pidFileInfo, nil)
	store.EXPECT().
		FullPath(lockFileName).Times(2).
		Return(lockFilePath)
	store.EXPECT().
		FullPath(pidFileName).
		Return(pidFilePath)
	store.EXPECT().
		GetWorkDir().
		Return(workDir)
	store.EXPECT().
		Exists(pidFileName).Return(true)
	store.EXPECT().
		Delete(pidFileName).Return(nil)

	// mock LockManager
	manager := NewMockLockManager(ctrl)
	manager.EXPECT().DiscoverByLockName(lockName).Return(nil, nil)
	manager.EXPECT().DiscoverByLockName(lockName).Return(info, nil)

	plock4, perr := prepareLock(lockName, info.PID, store, manager)
	req.NoError(perr)
	req.NotEqual(info, plock4.Info())

	// create a different lock file to simulate another process created the lock file
	// it will be different to the pidFileInfo
	lockFile, err := os.Create(plockFilePath(info.Name, workDir)) // create a new file
	req.NoError(err)
	lockFileInfo, err := lockFile.Stat()
	req.NoError(err)

	// mock link command to return lockFileInfo
	store.EXPECT().
		Link(plockPidFilename(lockName, info.PID), plockFileName(lockName)).
		Return(lockFileInfo, nil) // return the new file, which should trigger that lockFileInfo and pidFileInfo are not same

	// if linking returns wrong file info it should fail during verify
	perr = plock4.Acquire()
	req.Error(perr)
}

func TestPlockLocal_TryAcquire_Fail_With_Directory_Permission_Change(t *testing.T) {
	req := require.New(t)

	currentUser, err := user.Current()
	req.NoError(err)
	if currentUser.Uid == "0" { // we can only test permission issue if the test is not run using root user
		t.Skipf("cannot run permission related test as root user (%s)", currentUser)
		return
	}

	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	pid := info.PID
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())

	// fail to Acquire if directory permission changes after initialization
	stat, _ := os.Stat(workDir)
	t.Log(workDir, stat.Mode())
	os.Chmod(workDir, os.ModeDir)
	perr = plock.TryAcquire(time.Millisecond * 300) // timout value must be bigger than the default retry delay(i.e. 200ms)
	req.Error(perr)                                 // timeout error
	req.Equal("failed to acquire lock after timout: 300ms", perr.Error())
}

func TestPlockLocal_TryAcquire(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	pid := info.PID
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())

	// acquire should succeed
	perr = plock.TryAcquire(time.Second * 1)
	req.NoError(perr)

	// timeout when trying to require same lock
	plock2, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)

	// timout value less than the default retry delay(i.e. 200ms) should error
	perr = plock2.TryAcquire(time.Millisecond * 10)
	req.Error(perr)
	req.Equal("timeout '10ms' must be bigger than default retry delay", perr.Error())

	// timout value bigger than the default retry delay(i.e. 200ms) should cause timeout error
	perr = plock2.TryAcquire(time.Millisecond * 300)
	req.Error(perr)
	req.Equal("failed to acquire lock after timout: 300ms", perr.Error())

	// releasing lock should allow to reacquire
	perr = plock.Release()
	req.NoError(perr)
	perr = plock2.TryAcquire(time.Second * 1)
	req.NoError(perr)
}

func TestPlockLocal_Acquire_With_Race(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	// create n co-routines to compete to get the lock
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	notify := make(chan bool)
	var plocks []Lock
	count := 1000 // we tested it with 10K and that worked OK
	for i := 0; i < count; i++ {
		wg.Add(1)

		// try to make mock pid at the end of the pid ranges
		// However, it doesn't matter if it matches with an existing pid since we don't kill it
		mockPid := 20000 + i

		// instantiate a plock first outside the go routine to avoid race conditions
		plock, perr := NewLock(lockName, workDir, mockPid)
		req.NoError(perr)
		req.NotEqual(info, plock.Info())
		plocks = append(plocks, plock)

		go func(ctx context.Context, plock Lock, notify chan bool) {
			// start the worker
			for {
				select {
				case <-notify:
					plock.Acquire()
					wg.Done()
					break
				case <-ctx.Done():
					return
				}
			}
		}(ctx, plock, notify)
	}

	for i := 0; i < count; i++ {
		notify <- true
	}

	wg.Wait() // wait for all coroutines to finish lock acquire attempts

	totalActive := 0
	var activePlock Lock
	for _, plock := range plocks {
		if plock.IsAcquired() {
			totalActive++
			activePlock = plock
			t.Log("Active Lock", plock.Info())
		}
	}
	req.Equal(1, totalActive, "Active log was expected to be 1")
	if activePlock == nil {
		t.Fatalf("Total active lock was expected to be 1")
	}

	// check that lock file and temp PID file are same
	lockPath := plockFilePath(lockName, workDir)
	tmpPidPath := plockPidFilePath(lockName, workDir, activePlock.Info().PID)
	fileInfo, err := os.Lstat(lockPath)
	req.NoError(err)
	req.NotNil(fileInfo)
	pidFileInfo, err := os.Lstat(tmpPidPath)
	req.NoError(err)
	req.NotNil(pidFileInfo)
	req.True(os.SameFile(fileInfo, pidFileInfo))

	// check there is only one temp PID file
	list, perr := getFileList(workDir, lockName)
	req.NoError(perr)
	req.Equal(2, len(list))

	// check for same activated timestamp and file creation time
	req.Equal(fileInfo.ModTime().Format(time.RFC3339), activePlock.Info().ActivatedAt.Format(time.RFC3339))
}

func TestPlockLocal_Release(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	pid := info.PID
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())

	// before acquire release shouldn't pass
	perr = plock.Release()
	req.Error(perr)

	// acquire the lock
	if err := plock.Acquire(); err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	perr = plock.Release()
	req.NoError(perr)

	// check that lock files doesn't exist anymore
	req.False(fileExists(plock.Info().LockFilePath))
	req.False(fileExists(plock.Info().PidFilePath))
}

func TestPlockLocal_Release_Delete_Failure(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lockName := "test"
	pid := 10000
	workDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(workDir)

	store := NewMockfileStore(ctrl)
	activatedAt, _ := time.Parse(time.RFC3339, time.RFC3339)
	lock := &plock{ // mock a Lock to be able to access private members
		providerType: ProviderLocal,
		name:         lockName,
		pid:          pid,
		workDir:      workDir,
		lockFileName: plockFileName(lockName),
		pidFileName:  plockPidFilename(lockName, pid),
		store:        store,
		activatedAt:  &activatedAt,
	}

	store.EXPECT().Exists(lock.pidFileName).Return(true)
	store.EXPECT().Delete(lock.pidFileName).Return(errors.New("delete error"))
	perr := lock.Release()
	req.Error(perr)

	store.EXPECT().Exists(lock.pidFileName).Return(true)
	store.EXPECT().Delete(lock.pidFileName).Return(nil)
	store.EXPECT().Exists(lock.lockFileName).Return(true)
	store.EXPECT().Delete(lock.lockFileName).Return(errors.New("delete error"))
	perr = lock.Release()
	req.Error(perr)
}

func TestPlockLocal_Info(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	pid := os.Getpid()
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.NotEqual(info, plock.Info())

	// acquire the lock
	perr = plock.Acquire()
	req.NoError(perr)
	defer plock.Release()

	actual := plock.Info()
	req.True(plock.IsAcquired())
	req.Equal(info.PID, actual.PID)
	req.Equal(info.LockFilePath, actual.LockFilePath)
	req.Equal(info.Name, actual.Name)

}

func TestPlockLocal_IsAcquired(t *testing.T) {
	req := require.New(t)
	helper := testHelper{}
	lockName, workDir, info := helper.prepLockTestInfo(t)
	defer os.RemoveAll(workDir)

	pid := info.PID
	plock, perr := NewLock(lockName, workDir, pid)
	req.NoError(perr)
	req.False(plock.IsAcquired())

	// acquire the lock
	perr = plock.Acquire()
	req.NoError(perr)
	defer plock.Release()

	req.True(plock.IsAcquired())
}
