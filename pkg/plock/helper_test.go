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
	"golang.hedera.com/solo-provisioner/pkg/sanity"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testHelper ines various helper functions for tests
type testHelper struct{}

// prepLockTestInfo just prepares some values to test lock operations
func (th *testHelper) prepLockTestInfo(t *testing.T) (lockName string, workDir string, info *Info) {
	var err error
	req := require.New(t)

	tmpDir := t.TempDir()
	workDir, err = os.MkdirTemp(tmpDir, "plock*")
	req.NoError(err)

	lockName = "test"
	pid := os.Getpid()
	fullPath := filepath.Join(workDir, plockFileName(lockName))

	info = &Info{
		PID:          pid,
		Name:         lockName,
		WorkDir:      workDir,
		LockFileName: plockFileName(lockName),
		LockFilePath: fullPath,
		PidFileName:  plockPidFilename(lockName, pid),
		PidFilePath:  plockPidFilePath(lockName, tmpDir, pid),
		ProviderType: ProviderLocal,
	}
	return lockName, workDir, info
}

// createStaleLock returns a mock lock Info
func (th *testHelper) createStaleLock(t *testing.T, tmpDir string) *Info {
	return th.createTestLock(t, 20000000, tmpDir) // pid 20000000 should be stale
}

// createTestLock returns a mock lock Info
func (th *testHelper) createTestLock(t *testing.T, pid int, workDir string) *Info {
	req := require.New(t)
	var err error
	now := time.Now()
	lockName, err := sanity.Filename(fmt.Sprintf("mocklock%d", now.Nanosecond()))
	req.NoError(err)

	lockFilePath := plockFilePath(lockName, workDir)
	pidFilePath := plockPidFilePath(lockName, workDir, pid)

	_, err = os.Create(pidFilePath)
	req.NoError(err)

	err = os.Link(pidFilePath, lockFilePath)
	req.NoError(err)

	info := &Info{
		PID:          pid,
		Name:         lockName,
		WorkDir:      workDir,
		LockFileName: plockFileName(lockName),
		LockFilePath: lockFilePath,
		PidFileName:  plockPidFilename(lockName, pid),
		PidFilePath:  pidFilePath,
		ProviderType: ProviderLocal,
		ActivatedAt:  &now,
	}

	return info
}
