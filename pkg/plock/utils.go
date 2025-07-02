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
	"github.com/cockroachdb/errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// plockFileName returns the lock file name with extension
func plockFileName(lockName string) string {
	return fmt.Sprintf("%s%s", lockName, LockFileExtension)
}

// plockPidFileSuffix returns the file suffix of a pid file
func plockPidFileSuffix(pid int) string {
	return strings.Join([]string{
		PidSeparator,
		strconv.Itoa(pid),
		LockFileExtension,
	}, "")
}

// plockPidFilename returns the temp lock file name to be created
// It returns name like: {lockName}.{pid}.plock
func plockPidFilename(lockName string, pid int) string {
	return strings.Join([]string{
		lockName,
		plockPidFileSuffix(pid),
	}, "")
}

// plockFilePath returns the full path to the plock file
func plockFilePath(lockName string, workDir string) string {
	return filepath.Join(workDir, plockFileName(lockName))
}

// plockFilePath returns the full path to the temp plock PID file
func plockPidFilePath(lockName string, workDir string, pid int) string {
	return filepath.Join(workDir, plockPidFilename(lockName, pid))
}

// fileExists checks if a file exist or not in local filesystem
// It uses os.Stat so if the path is a symlink it will follow the link and check its existence.
func fileExists(path string) bool {
	_, err := os.Stat(path) // We use os.Stat instead of os.Lstat to ensure we follow symlinks
	if err == nil {
		return true
	}
	return false
}

// getFileList returns a list of files in a directory
// If substr is not an empty string it tries to find file names that contains the substr
func getFileList(dirPath string, substr string) ([]string, error) {
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot access work dir: %s", dirPath)
	}
	defer dir.Close()

	fullList, _ := dir.Readdirnames(0) // 0 to read all files and folders
	if substr == "" {
		return fullList, nil
	}

	// apply filter
	var list []string
	for _, fileName := range fullList {
		if strings.Contains(fileName, substr) {
			list = append(list, fileName)
		}
	}
	return list, nil
}

// splitPidFilePath splits a full lock file path into work directory, lock name and pid
func splitPidFilePath(pidFilePath string) (workDir string, lockName string, pid int, err error) {
	workDir = ""
	lockName = ""
	pid = InvalidPID

	workDir, fileName := filepath.Split(pidFilePath)
	workDir = filepath.Clean(workDir)

	fileNameParts := strings.Split(fileName, PidSeparator)
	if len(fileNameParts) != 3 {
		err = errors.Newf("invalid pid lock file", fileName)
		return
	}

	lockName = fileNameParts[0]
	pidStr := fileNameParts[1]
	if pid, err = strconv.Atoi(pidStr); err != nil {
		err = errors.Newf("invalid pid format in lock file", fileName, pidStr, err.Error())
		return
	}

	return
}

// isStalePID returns true if the process with the given pid is stale
func isStalePID(pid int) bool {
	proc, _ := os.FindProcess(pid)
	// check if the process is actually running or not
	// If  sig  is 0, then no signal is sent, but error checking is still per‐formed; so we can use it check for the
	// existence of a process ID  or process group ID.
	err := proc.Signal(syscall.Signal(0))
	if err != nil {
		return true
	}

	return false
}

// containsStr returns true if item exists in the list
func containsStr(list []string, item string) bool {
	for _, i := range list {
		if i == item {
			return true
		}
	}

	return false
}
