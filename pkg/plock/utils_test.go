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
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestFileExists(t *testing.T) {
	req := require.New(t)
	req.False(fileExists(".INVALID__"))
	tmpFile, err := os.CreateTemp(os.TempDir(), "plock-test*")
	req.NoError(err)
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()
	req.True(fileExists(tmpFile.Name()))
}

func TestGetFileList(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test*")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)
	tmpFile, err := os.CreateTemp(tmpDir, "plock-util")
	req.NoError(err)
	tmpFile.Close()

	// invalid dir error
	_, perr := getFileList("INVALID_DIR", "plock-util")
	req.Error(perr)

	tmpFile, err = os.CreateTemp(tmpDir, "other-util")
	req.NoError(err)
	tmpFile.Close()
	list, perr := getFileList(tmpDir, "")
	req.NoError(perr)
	req.Equal(2, len(list))

	list, perr = getFileList(tmpDir, "plock-util")
	req.NoError(perr)
	req.Equal(1, len(list))
}

func TestSplitPidFilePath(t *testing.T) {
	req := require.New(t)
	workDir, lockName, pid, perr := splitPidFilePath(plockPidFilePath("test", "/mock", 1))
	req.NoError(perr)
	req.Equal("/mock", workDir)
	req.Equal("test", lockName)
	req.Equal(1, pid)

	// invalid format error
	_, _, _, perr = splitPidFilePath("/mock/test.1g5.plock")
	req.Error(perr)
}

func TestContains(t *testing.T) {
	req := require.New(t)
	req.False(containsStr(nil, "a")) // non-array should fail
	req.True(containsStr([]string{"a", "b", "c"}, "a"))
	req.False(containsStr([]string{"a", "b", "c"}, "aa"))
}
