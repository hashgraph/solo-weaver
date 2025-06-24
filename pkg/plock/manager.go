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
	"github.com/mitchellh/go-ps"
	"golang.hedera.com/solo-provisioner/pkg/erx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// plockManager is the default implementation of LockManager API
type plockManager struct {
	store fileStore
}

// NewLockManager returns an instance of LockManager to manage multiple process locks
func NewLockManager(workDir string) (LockManager, error) {
	store, err := newLocalFileStore(workDir)
	if err != nil {
		return nil, err
	}

	return newLockManagerWithStore(store)
}

// newLockManagerWithStore returns an instance of plockManager with the given store
func newLockManagerWithStore(store fileStore) (*plockManager, error) {
	if store == nil {
		return nil, erx.NewIllegalArgumentError(
			nil,
			"store",
			"file store cannot be nil",
			store)
	}

	return &plockManager{
		store: store,
	}, nil
}

// SetWorkDir sets a new work directory
func (pm *plockManager) SetWorkDir(workDir string) error {
	return pm.store.SetWorkDir(workDir)
}

// GetWorkDir returns the current work directory
func (pm *plockManager) GetWorkDir() string {
	return pm.store.GetWorkDir()
}

// Discover discovers all existing locks in the given work directory
func (pm *plockManager) Discover(maxCount int) (map[int]*Info, error) {
	list, err := pm.store.List(pm.GetWorkDir(), LockFileExtension, maxCount)
	if err != nil {
		return nil, erx.NewLockError(err, fmt.Sprintf("Could not read file list from work directory: %s", pm.GetWorkDir()))
	}

	locks := map[int]*Info{}
	for _, file := range list {
		info, _ := pm.pidFileToLockInfo(pm.store.FullPath(file.Name()))
		if info != nil { // we ignore any errors and are just interested in valid entries
			locks[info.PID] = info
		}
	}
	return locks, nil
}

func (pm *plockManager) DiscoverStaleLocks(maxCount int) (map[int]*Info, error) {
	locks, err := pm.Discover(maxCount)
	if err != nil {
		return nil, err
	}

	staleLocks := map[int]*Info{}
	for _, lock := range locks {
		if isStalePID(lock.PID) {
			staleLocks[lock.PID] = lock
		}
	}

	return staleLocks, nil
}

func (pm *plockManager) DiscoverByPID(pid int) ([]*Info, error) {
	fileList, err := pm.store.List(pm.GetWorkDir(), plockPidFileSuffix(pid), -1)
	if err != nil {
		return nil, erx.NewLockError(err, fmt.Sprintf("Could not read file list from work directory: %s", pm.GetWorkDir()))
	}

	var locks []*Info
	for _, file := range fileList {
		info, _ := pm.pidFileToLockInfo(pm.store.FullPath(file.Name()))
		if info != nil { // we ignore any errors and are just interested in valid entries
			locks = append(locks, info)
		}
	}

	return locks, nil
}

func (pm *plockManager) DiscoverByLockName(lockName string) (*Info, error) {
	list, err := pm.store.List(pm.GetWorkDir(), lockName, -1)
	if err != nil {
		return nil, erx.NewLockError(err, fmt.Sprintf("Could not read file list from work directory: %s", pm.GetWorkDir()))
	}

	for _, file := range list {
		fileNameParts := strings.Split(file.Name(), PidSeparator)
		if len(fileNameParts) == 3 && fileNameParts[0] == lockName { // pid file will have three parts
			return pm.pidFileToLockInfo(filepath.Join(pm.GetWorkDir(), file.Name())) // there should be only one, so just return
		}
	}

	return nil, erx.NewLockError(nil, fmt.Sprintf("Could not find lock with name: %s", lockName))
}

func (pm *plockManager) ResetStaleLock(info Info) error {
	if !isStalePID(info.PID) {
		return erx.NewLockError(nil, fmt.Sprintf("Lock's PID is not stale and cannot be reset: %s", strconv.Itoa(info.PID)))
	}

	pidFileName := plockPidFilename(info.Name, info.PID)
	if pm.store.Exists(pidFileName) {
		err := pm.store.Delete(pidFileName)
		if err != nil {
			return erx.NewLockError(err, fmt.Sprintf("Could not remove pid file: %s", info.PidFilePath))
		}
	}

	lockFileName := plockFileName(info.Name)
	if pm.store.Exists(lockFileName) {
		err := pm.store.Delete(lockFileName)
		if err != nil {
			return erx.NewLockError(err, fmt.Sprintf("Could not remove lock file: %s", info.LockFilePath))
		}
	}

	return nil
}

func (pm *plockManager) ResetStaleLocks() error {
	staleLocks, err := pm.DiscoverStaleLocks(-1)
	if err != nil {
		return err
	}

	for _, lock := range staleLocks {
		err = pm.ResetStaleLock(*lock)
		if err != nil {
			return err
		}
	}

	return nil
}

func (pm *plockManager) ResetLock(pid int, killable []string) error {
	if isStalePID(pid) {
		locks, err := pm.DiscoverByPID(pid)
		if err != nil {
			return err
		}

		for _, info := range locks {
			err = pm.ResetStaleLock(*info)
			if err != nil {
				return err
			}
		}

		return nil // done processing stale locks
	}

	// since it is an active pid, we should be careful of killing it.
	// it should kill only if the process is of allowed type matching executable name in the killable arg.
	process, err := ps.FindProcess(pid)
	if err != nil {
		return nil
	}
	for _, exe := range killable {
		if exe == process.Executable() {
			proc, _ := os.FindProcess(pid)
			proc.Kill() // send kill signal.
			return nil
		}
	}

	return erx.NewLockError(
		nil,
		fmt.Sprintf("ResetLock cannot be applied for executable: %s with active PID: %d", process.Executable(), pid),
	)
}

// getFileModifiedAt returns the file modification time
func (pm *plockManager) getFileModifiedAt(fileName string) (*time.Time, error) {
	fileInfo, err := pm.store.Stat(fileName)
	if err != nil {
		return nil, erx.NewLockError(err, fmt.Sprintf("Cannot read the file info for corresponding PID file: %s", fileName))
	}
	modifiedAt := fileInfo.ModTime()

	return &modifiedAt, nil
}

// pidFileToLockInfo extracts plock Info from the lock file path
func (pm *plockManager) pidFileToLockInfo(pidFilePath string) (*Info, error) {
	workDir, lockName, pid, err := splitPidFilePath(pidFilePath)
	if err != nil {
		return nil, erx.NewLockError(err, fmt.Sprintf("Cannot parse PidFilePath: %s", pidFilePath))
	}

	if pid > 0 { // only process when there is a valid PID
		lockFilePath := plockFilePath(lockName, workDir)

		lockFileName := plockFileName(lockName)
		modifiedAt, err := pm.getFileModifiedAt(lockFileName)
		if err != nil {
			return nil, err
		}

		// prepare Info
		return &Info{
			PID:          pid,
			Name:         lockName,
			WorkDir:      workDir,
			LockFilePath: lockFilePath,
			PidFilePath:  pidFilePath,
			ProviderType: pm.store.ProviderType(),
			ActivatedAt:  modifiedAt, // we assume the file hasn't been modified after it is created.
		}, nil
	}

	return nil, erx.NewLockError(nil, fmt.Sprintf("invalid pid lock file path: %s", pidFilePath))
}
