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
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/pkg/sanity"
	"os"
	"sync"
	"time"
)

// plock is the default implementation of Lock API
//
// It provides process locks using Unix based file system
// Based on the fileStore implementation it can work as local Lock or remote Lock
type plock struct {
	providerType string
	name         string
	pid          int // negative PID is invalid
	workDir      string
	lockFileName string
	pidFileName  string
	store        fileStore
	manager      LockManager
	activatedAt  *time.Time
}

// prepareLock prepares an instance of plock with required configuration
func prepareLock(lockName string, pid int, store fileStore, manager LockManager) (*plock, error) {
	var err error

	lockName, err = sanity.Filename(lockName)
	if lockName == "" || err != nil {
		return nil, errors.Wrapf(err, "invalid lock name: %s", err.Error())
	}

	if pid == InvalidPID || pid < 0 {
		pid = os.Getpid()
	}

	if store == nil {
		return nil, errors.New("store cannot be empty")
	}

	if manager == nil {
		return nil, errors.New("lock manager cannot be empty")
	}

	return &plock{
		pid:          pid,
		name:         lockName,
		workDir:      store.GetWorkDir(),
		lockFileName: plockFileName(lockName),
		pidFileName:  plockPidFilename(lockName, pid),
		providerType: store.ProviderType(),
		store:        store,
		manager:      manager,
		activatedAt:  nil,
	}, nil
}

// NewLock returns an instance of Lock
//
// lockName should be the unique lock name that is tracked by the process for mutual exclusion. It is usually the
// program name.The actual lock file will be created as: {workDir}/{lockName}.plock
//
// workDir is the directory where the lock files should be created. By default, it is set to DefaultLocalWorkDir.
//
// pid is the process ID that Lock should create a PID file for. A negative or InvalidPID for pid argument will force it to use
// correct pid of the process as received by invoking os.Getpid()
func NewLock(lockName string, workDir string, pid int) (Lock, error) {
	store, err := newLocalFileStore(workDir)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot instantiate work dir '%s': %s", workDir, err.Error())
	}

	manager := &plockManager{
		store: store,
	}

	return prepareLock(lockName, pid, store, manager)
}

// Acquire tries to create PID lock file
//
// In order to identify stale locks where the process didn't clean up the file, we need to store the PID with the lock
// file so that we can clean those stale locks later. However, this creates problem as atomicity in creating a lockfile
// as well as saving it with PID information is error-prone because of race conditions.
//
// Therefore, we use two-phase commit protocol to create the lock file.
// In the PREP phase, it creates a temp file with PID information. This phase does not have race condition.
// In the COMMIT phase, it tries to HardLink that file with the desired lock file. This phase has race condition
// which may or may not succeed. So after HardLink command, it checks if lockfile actually points to the desired
// temp PID file or not.
//
// One simple optimization we applied compared to other solutions is that we embed the PID in temp file's name, i.e.
// {workDir}/{lockName}.{PID}.plock, instead of writing it as the file content. Thus, we don't need to read the file
// content for PID information while we do scanning and garbage collection of stale lock files.
//
// So the pseudocode for acquiring lock is as below:
// - CHECK: Check if the lockfile exists with name: {workDir}/{lockName}.plock, and if it does not exist then continue.
// - PREP: Create a tmp file with PID: {workDir}/{lockName}.{PID}.plock
// - COMMIT: Hardlink the desired lockfile to the tmp file: {workDir}/{lockName}.plock -> {workDir}/{lockName}.{PID}.plock
// - VERIFY: Ensure HardLink points to the correct PID file (this is to double-check that it didn't lose in the race)
// - SUCCESS: Mark it as activated
func (pl *plock) Acquire() error {
	var err error
	defer pl.cleanUpAcquireFailure()

	// if it is already acquired, it should have been released first
	if pl.IsAcquired() {
		return errorx.IllegalState.New("Attempting to acquire the lock failed: lock '%s' is already acquired", pl.Info().String())
	}

	// CHECK: Check if the lockfile exists with name: {workDir}/{lockName}.plock, and if it does not exist then continue.
	existingLock, _ := pl.manager.DiscoverByLockName(pl.name)
	if existingLock != nil {
		return errorx.IllegalState.New("Attempting to acquire the lock failed: lockfile '%s' already exists", existingLock)
	}

	// PREP: Create a tmp file with PID: {workDir}/{lockName}.{PID}.plock
	pidFileInfo, err := pl.store.Create(pl.pidFileName)
	if err != nil {
		return errorx.IllegalState.New("Failed to create PID file: %s", pl.pidFileName)
	}

	// COMMIT: Hardlink the lockfile to the tmp file: {workDir}/{lockName}.plock -> {workDir}/{lockName}.{PID}.plock
	// During race any competing process may win at this step. The loosing process therefore must stop and return error.
	lockFileInfo, err := pl.store.Link(pl.pidFileName, pl.lockFileName)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// ok, so we failed in the race to acquire the lock
			existingLock, _ := pl.manager.DiscoverByLockName(pl.name)
			return errorx.IllegalState.
				New("lock exists for: %s. Existing locks: %s", pl.store.FullPath(pl.lockFileName), existingLock).
				WithUnderlyingErrors(err)

		}

		// unexpected IO error, this shouldn't happen
		return errorx.IllegalState.
			New("unexpected error while acquiring lock: %s", pl.store.FullPath(pl.lockFileName)).
			WithUnderlyingErrors(err)
	}

	// VERIFY: Ensure HardLink points to the correct PID file (this is to double-check that it didn't lose in the race)
	// This may look redundant since if we are here, it means technically it succeeded with the HardLinking. However,
	// there is nothing wrong to verify that it actually succeed.
	if !os.SameFile(lockFileInfo, pidFileInfo) {
		// we lost in the race somehow, so couldn't acquire the lock
		existingLock, _ := pl.manager.DiscoverByLockName(pl.name)
		return errorx.IllegalState.
			New("lock exists for: %s. Existing locks: %s", pl.store.FullPath(pl.lockFileName), existingLock)
	}

	// SUCCESS: Mark it as activated
	modifiedAt := lockFileInfo.ModTime()
	pl.activatedAt = &modifiedAt

	return nil
}

// TryAcquire tries to acquire a lock with a timeout
//
// timout value must be bigger than the default retry delay(i.e. 200ms). Ideally timeout should be in seconds.
func (pl *plock) TryAcquire(timeout time.Duration) error {
	if timeout < DefaultRetryDelay {
		return errors.Newf("timeout '%s' must be bigger than default retry delay", timeout.String())
	}

	ticker := time.NewTicker(DefaultRetryDelay)
	timeoutExpired := time.After(timeout)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for {
			select {
			case <-ticker.C:
				perr := pl.Acquire()
				if perr == nil && pl.IsAcquired() {
					wg.Done()
					return
				}
			case <-timeoutExpired:
				ticker.Stop()
				wg.Done()
				return
			}
		}
	}()
	wg.Wait()

	if pl.IsAcquired() {
		return nil
	}

	return errors.Newf("failed to acquire lock after timout: %s", timeout.String())
}

// Release releases the acquired process lock by cleaning up the PID lock file
func (pl *plock) Release() error {
	if !pl.IsAcquired() {
		return errors.Newf("lock is not acquired, so cannot be released yet")
	}

	if pl.store.Exists(pl.pidFileName) {
		err := pl.store.Delete(pl.pidFileName)
		if err != nil {
			return errors.Newf("failed to cleanup pid lock file", pl.pidFileName)
		}
	}

	if pl.store.Exists(pl.lockFileName) {
		err := pl.store.Delete(pl.lockFileName)
		if err != nil {
			return errors.Newf("failed to cleanup lock file", pl.lockFileName)
		}
	}

	pl.activatedAt = nil

	return nil
}

// Info returns information about the plock
func (pl *plock) Info() *Info {
	return &Info{
		PID:          pl.pid,
		Name:         pl.name,
		WorkDir:      pl.workDir,
		LockFileName: pl.lockFileName,
		LockFilePath: pl.store.FullPath(pl.lockFileName),
		PidFileName:  pl.pidFileName,
		PidFilePath:  pl.store.FullPath(pl.pidFileName),
		ProviderType: pl.providerType,
		ActivatedAt:  pl.activatedAt,
	}
}

// IsAcquired returns if the lock is active or not
func (pl *plock) IsAcquired() bool {
	return pl.activatedAt != nil
}

// cleanUpAcquireFailure removes any existing temp lock files and reset the plock instance
//
// WARNING: don't delete the lockfile here since it is acquired by another process and this method is supposed to
// be called only when lock couldn't be acquired
func (pl *plock) cleanUpAcquireFailure() {
	if !pl.IsAcquired() && pl.store.Exists(pl.pidFileName) {
		pl.store.Delete(pl.pidFileName)
	}
}
