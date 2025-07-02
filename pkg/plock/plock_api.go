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
	"os"
	"time"
)

// Lock handles process lock by using PID file locking mechanism
// It supports both local and remote locking based on various providers that implement its interface
//
// Currently it exposes blocking APIs only, which means a process needs to wait for the response before proceeding with
// next Acquire and/or Release commands. So it is not possible to execute multiple Acquire and Release commands in
// parallel.
//
// Async locking/unlocking may be supported in the future if there is a need.
type Lock interface {
	// Acquire attempts to set a process lock
	Acquire() error

	// TryAcquire attempts to set a process lock before the timeout expires
	TryAcquire(timeout time.Duration) error

	// Release releases the acquired process lock
	//
	// Note: It returns error if a lock has not been acquired yet. So IsAcquired() should be called before
	// calling Release(). It also returns error if the lock related files cannot be deleted.
	Release() error

	// Info returns information about the plock
	Info() *Info

	// IsAcquired returns if the lock is acquired or not
	IsAcquired() bool
}

// LockManager defines API for various general lock management functionalities
// This helps to manage multiple process Lock.
type LockManager interface {
	// SetWorkDir sets a new work directory
	SetWorkDir(workDir string) error

	// GetWorkDir returns the current work directory
	GetWorkDir() string

	// Discover discovers all existing locks in the given work directory
	// It returns a map: {PID: *Info}
	// It assumes that the file modification time is the lock activation time since those lock files shouldn't have been
	// modified by any other process.
	// if maxCount <= 0 it returns the maximum number of locks
	// if maxCount > 1 it returns the specified number of locks if available
	Discover(maxCount int) (map[int]*Info, error)

	// DiscoverStaleLocks discovers all stale locks in the work directory
	// It returns a map: {PID: *Info}
	// if maxCount <= 0 it returns the maximum number of locks
	// if maxCount > 1 it returns the specified number of locks if available
	DiscoverStaleLocks(maxCount int) (map[int]*Info, error)

	// DiscoverByPID returns all existing plock info for a pid
	DiscoverByPID(pid int) ([]*Info, error)

	// DiscoverByLockName discovers existing PID file in the local work directory for the given lock name
	// It returns on the first PID lock file it finds in the work directory
	// Note that plock expects both {lockName}.plock and {lockName}.{PID}.plock to exist for a valid lock
	DiscoverByLockName(lockName string) (*Info, error)

	// ResetStaleLock deletes the stale lock
	ResetStaleLock(info Info) error

	// ResetStaleLocks deletes all stale locks in the work directory
	// It stops at the first error and returns the error
	ResetStaleLocks() error

	// ResetLock forcefully reset an existing lock by killing the process
	// Note this is a destructive action and should be used with absolute care
	// As a precaution, it only kills processes if it is in its allowed process types. A further brainstorming is needed.
	//
	// killable is a list of executable names that are allowed to be killed.
	ResetLock(pid int, killable []string) error
}

// fileStore defines API of a plock store
// For local filesystem, dirPath represents the directory where the file should be created
// For remote S3 like object store, dirPath represents bucketName
type fileStore interface {
	// SetWorkDir sets the work directory
	// For local filesystem, dirPath represents the directory where the file should be created
	// For remote S3 like object store, dirPath represents bucketName
	SetWorkDir(dirName string) error

	// GetWorkDir returns the current work directory
	GetWorkDir() string

	// Stat returns os.FileInfo of the named file
	Stat(fileName string) (os.FileInfo, error)

	// Exists returns true if the file exists
	Exists(fileName string) bool

	// Create creates a new file
	Create(fileName string) (os.FileInfo, error)

	// Delete deletes the file
	Delete(fileName string) error

	// Link creates a hard link of the oldFile using the newFile
	// If it fails because the file exists it must return os.ErrExist
	Link(oldFile string, newFile string) (os.FileInfo, error)

	// List returns a list of files in the specified directory
	// Note this is a costly operation and should be used with care
	//
	// If substr is not an empty string it tries to find file names that contains the substr
	// if maxCount < 0, it returns all files in the directory
	// if maxCount > 0, it returns at most maxCount number of files in the directory, if exists
	List(dirPath string, substr string, maxCount int) ([]os.FileInfo, error)

	// FullPath returns full path combining fileName and dirName
	FullPath(fileName string) string

	// ProviderType returns the type of the storage provider
	ProviderType() string
}
