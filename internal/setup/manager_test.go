/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
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

package setup

import (
	"bufio"
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"path/filepath"
	"testing"
)

const (
	testDataDir = "../../../../test/data"
)

func TestNewManager(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	sm, err := NewManager("")
	req.NoError(err)
	req.NotNil(sm)
}

func TestCreateStagingArea(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	tmpFile, err := os.CreateTemp("", "test.log")
	req.NoError(err)

	sm, err := NewManager("install", WithLogger(logx.Nop()))
	req.NoError(err)
	req.NotNil(sm)

	// Test that the working directory is created and that the logger is updated with the path.
	err = sm.CreateStagingArea()
	req.NoError(err)
	f, err := os.Open(tmpFile.Name())
	req.NoError(err)
	defer f.Close()
	defer os.RemoveAll(tmpFile.Name())
	reader := bufio.NewReader(f)

	line, _, err := reader.ReadLine() // logger configuration is first row
	req.NoError(err)
	line, _, err = reader.ReadLine() // working directory is second row
	req.NoError(err)
	req.Contains(string(line), logFields.workingDirectory)

	// test that the working directory exists
	workingDirectory := sm.GetInstallWorkingDirectory()
	req.NotEmpty(workingDirectory)
	fsm, err := fsx.NewManager()
	req.NoError(err)
	req.True(fsm.IsDirectory(workingDirectory))

	// perform the cleanup and retain the temp directory for troubleshooting
	os.Setenv("NMT_RETAIN_TEMP", "true")
	err = sm.Cleanup()
	req.NoError(err)
	_, found, err := fsm.PathExists(workingDirectory)
	req.NoError(err)
	req.True(found)
	req.NotEmpty(sm.GetInstallWorkingDirectory())
	os.Setenv("NMT_RETAIN_TEMP", "")

	// perform the cleanup and verify the directory has been deleted
	err = sm.Cleanup()
	req.NoError(err)
	_, found, err = fsm.PathExists(workingDirectory)
	req.NoError(err)
	req.False(found)
	req.Empty(sm.GetInstallWorkingDirectory())
}

func testArchiveExtraction(t *testing.T, testFileName string, errorMessage string) {
	t.Helper()
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)
	ctx := context.Background()

	sm, err := NewManager("")
	req.NoError(err)

	archive := filepath.Join(testDataDir, testFileName)

	err = sm.CreateStagingArea()
	req.NoError(err)

	err = sm.ExtractSDKArchive(ctx, archive)
	if errorMessage == "" {
		req.NoError(err)
		extractedTarArchive := filepath.Join(sm.GetInstallWorkingDirectory(), sdkPackageDirName, "test_folder", "text-file.txt")
		req.FileExists(extractedTarArchive)
	} else {
		req.ErrorContains(err, errorMessage)
	}
	err = sm.Cleanup()
	req.NoError(err)
}

func TestExtractSDKArchive(t *testing.T) {
	testCase := []struct {
		archiveFileName string
		errorMessage    string
	}{
		{"test.tar.gz", ""},
		{"test.zip", ""},
		{"test.tar", ""},
		{"invalid-archive", "package file does not exist or is not a regular file"},
		{"test-bad.zip", "failed to extract SDK package"},
	}
	for _, tc := range testCase {
		testArchiveExtraction(t, tc.archiveFileName, tc.errorMessage)
	}
}

func TestExtractSDKArchiveNoCreateStagingArea(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)
	ctx := context.Background()

	sm, err := NewManager("")
	req.NoError(err)

	err = sm.ExtractSDKArchive(ctx, "invalid-archive")
	req.ErrorContains(err, "working directory not set")
}

func testPrepareDocker(t *testing.T, testCaseName string, imageID string, errorMessage string) {
	t.Helper()
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	assertFailureMessage := fmt.Sprintf("testCaseName: %s, imageID: %s, errorMessage: %s", testCaseName, imageID, errorMessage)
	sm, err := NewManager("")
	req.NoError(err, assertFailureMessage)

	err = sm.CreateStagingArea()
	req.NoError(err, assertFailureMessage)

	err = sm.PrepareDocker(imageID)
	if errorMessage == "" {
		req.NoError(err, assertFailureMessage)
	} else {
		req.ErrorContains(err, errorMessage, assertFailureMessage)
	}
	err = sm.Cleanup()
	req.NoError(err, assertFailureMessage)
}

func TestPrepareDocker(t *testing.T) {
	testCase := []struct {
		testCaseName string
		imageID      string
		errorMessage string
	}{
		{"happyMain", "main", ""},
		{"happyJRS", "jrs", ""},
		{"badImageName", "not-an-image", "failed to prepare Docker Image Build Environment"},
	}
	for _, tc := range testCase {
		testPrepareDocker(t, tc.testCaseName, tc.imageID, tc.errorMessage)
	}
}

func TestPrepareDockerNoCreateStagingArea(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	sm, err := NewManager("")
	req.NoError(err)

	err = sm.PrepareDocker("main")
	req.ErrorContains(err, "failed to prepare Docker Image Build Environment")
}

func TestPrepareDocker_ValidateExtractedImages(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	sm, err := NewManager("")
	req.NoError(err)

	err = sm.CreateStagingArea()
	req.NoError(err)

	err = sm.PrepareDocker("main")
	req.NoError(err)

	installWorkingDirectory := sm.GetInstallWorkingDirectory()
	mainNetworkNodeDir := filepath.Join(installWorkingDirectory, dockerImageDirName, "main-network-node")
	req.DirExists(mainNetworkNodeDir)
	req.DirExists(filepath.Join(mainNetworkNodeDir, sdkDirName))
	req.FileExists(filepath.Join(mainNetworkNodeDir, "Dockerfile"))

	networkNodeBaseDir := filepath.Join(installWorkingDirectory, dockerImageDirName, "network-node-base")
	req.DirExists(networkNodeBaseDir)
	req.NoDirExists(filepath.Join(networkNodeBaseDir, sdkDirName))
	req.FileExists(filepath.Join(networkNodeBaseDir, "Dockerfile"))

	networkNodeHavegedDir := filepath.Join(installWorkingDirectory, dockerImageDirName, "network-node-haveged")
	req.DirExists(networkNodeHavegedDir)
	req.NoDirExists(filepath.Join(networkNodeHavegedDir, sdkDirName))
	req.FileExists(filepath.Join(networkNodeHavegedDir, "Dockerfile"))

	dockerImageName := sm.GetDockerNodeImageName()
	req.Equal("main-network-node", dockerImageName)

	err = sm.Cleanup()
	req.NoError(err)
}

// TODO more test coverage
