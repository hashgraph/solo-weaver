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

package paths

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScout_FindWorkDir(t *testing.T) {
	req := require.New(t)
	exFile, err := os.Executable()
	req.NoError(err)
	exPath, err := filepath.Abs(exFile)
	req.NoError(err)

	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	folder, err := scout.findWorkDir()
	req.NoError(err)
	req.NotEmpty(folder)

	req.Equal(filepath.Dir(exPath), folder)
}

func TestScout_FindExecutableName(t *testing.T) {
	req := require.New(t)
	exFile, err := os.Executable()
	req.NoError(err)
	_, expectedExName := filepath.Split(exFile)
	if idx := strings.Index(expectedExName, "."); idx >= 0 {
		expectedExName = expectedExName[0:idx]
	}

	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	actualExName := scout.findExecutableName()
	req.NoError(err)
	req.NotEmpty(actualExName)
	req.Equal(expectedExName, actualExName)

}

func TestScout_FindConfigFolder(t *testing.T) {
	req := require.New(t)
	rootDir, err := os.MkdirTemp(os.TempDir(), "nmt-test*") // WARNING: remember we try to delete the tmp dir
	req.NoError(err)

	tmpConfigDirName := fmt.Sprintf("configs_%d", time.Now().Nanosecond())
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).SetWorkDir(rootDir).SetConfigDirName(tmpConfigDirName).Build()
	_, err = scout.findConfigFolder(false)
	req.Error(err)

	folder, err := scout.findConfigFolder(true)
	req.NoError(err)
	req.Contains(folder, tmpConfigDirName)
	os.Remove(folder)
}

func TestScout_FindLogsFolder(t *testing.T) {
	req := require.New(t)
	rootDir, err := os.MkdirTemp(os.TempDir(), "nmt-test*") // WARNING: remember we try to delete the tmp dir
	req.NoError(err)

	tmpLogDirName := fmt.Sprintf("logs_%d", time.Now().Nanosecond())
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).SetWorkDir(rootDir).SetLogDirName(tmpLogDirName).Build()
	_, err = scout.findConfigFolder(false)
	req.Error(err)

	folder, err := scout.findLogFolder(true)
	req.NoError(err)
	req.Contains(folder, tmpLogDirName)
	os.Remove(folder)
}

func TestScout_DiscoverPaths(t *testing.T) {
	req := require.New(t)

	// create the work dir
	rootDir := t.TempDir()
	workDir := filepath.Join(rootDir, HederaAppDirName, NodeMgmtDirName, "bin")
	err := os.MkdirAll(workDir, DefaultDirMode)
	req.NoError(err)

	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).SetWorkDir(workDir).Build()
	assertHederaAppPaths(t, scout, rootDir, true)  // create directories should succeed
	assertHederaAppPaths(t, scout, rootDir, false) // do not create but see if we can discover those
}

func TestScout_DiscoverHederaAppDir(t *testing.T) {
	req := require.New(t)
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	nmtBinDir := filepath.Join(mockNodeMgmtDirRoot(), "bin")
	hgcapp, err := scout.discoverHederaAppDir(nmtBinDir, false)
	req.NoError(err)

	hgcappDir := mockHgAppDirRoot()
	nodeMgmtDir := mockNodeMgmtDirRoot()

	req.NotEmpty(hgcapp)
	req.Equal(hgcappDir, hgcapp.Root)
	req.Equal(nodeMgmtDir, hgcapp.NodeMgmtTools.Root)
	assertNodeMgmtPaths(t, hgcappDir, hgcapp.NodeMgmtTools)
}

func TestScout_DiscoverNodeMgmtDir(t *testing.T) {
	req := require.New(t)
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	hgcappRootDir := mockHgAppDirRoot()
	nmtDir, err := scout.discoverNodeMgmtDir(hgcappRootDir, false)
	req.NoError(err)

	assertNodeMgmtPaths(t, hgcappRootDir, nmtDir)
}

func TestScout_DiscoverServicesUpgradeDir(t *testing.T) {
	req := require.New(t)
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	svcRoot := filepath.Join(mockHapiAppDirRoot(), "/data")
	upgradeDir, err := scout.discoverUpgradeDir(svcRoot, false)
	req.NoError(err)

	assertUpgradePaths(t, svcRoot, upgradeDir)
}

func TestScout_DiscoverHapiAppDataDir(t *testing.T) {
	req := require.New(t)
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	hapiAppRoot := mockHapiAppDirRoot()
	dataDir, err := scout.discoverHapiAppDataDir(hapiAppRoot, false)
	req.NoError(err)
	assertHapiAppDataPaths(t, hapiAppRoot, dataDir)
}

func TestScout_DiscoverHapiAppDir(t *testing.T) {
	req := require.New(t)
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	svcRoot := mockHederaServicesRoot()
	hapiAppDir, err := scout.discoverHapiAppDir(svcRoot, false)
	req.NoError(err)
	assertHapiAppPaths(t, svcRoot, hapiAppDir)
}

func TestScout_DiscoverHederaServicesDir(t *testing.T) {
	req := require.New(t)
	scout := NewScoutBuilder(context.Background(), getTestLogger(t)).Build()

	hgAppRoot := mockHgAppDirRoot()
	svcRoot := mockHederaServicesRoot()
	svcDir, err := scout.discoverHederaServicesDir(hgAppRoot, false)
	req.NoError(err)
	req.Equal(svcRoot, svcDir.Root)
	assertHapiAppPaths(t, svcRoot, svcDir.HapiApp)
}
