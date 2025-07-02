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
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"path"
	"path/filepath"
	"testing"
)

func mockTempDirRoot() string {
	return filepath.Clean("../../tmp")
}

func mockHgAppDirRoot() string {
	return filepath.Clean("../../tmp/hgcapp")
}

func mockNodeMgmtDirRoot() string {
	return filepath.Clean(filepath.Join(mockHgAppDirRoot(), NodeMgmtDirName))
}

func mockHederaServicesRoot() string {
	return filepath.Clean(filepath.Join(mockHgAppDirRoot(), HederaServicesDirName))
}

func mockHapiAppDirRoot() string {
	return filepath.Clean(filepath.Join(mockHederaServicesRoot(), HederaApiDirName))
}

func getTestLogger(t *testing.T) *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

func assertHederaAppPaths(t *testing.T, scout *Scout, rootDir string, createIfNotFound bool) {
	req := require.New(t)
	hgcappDir := filepath.Join(rootDir, HederaAppDirName)
	nmtDir := filepath.Join(hgcappDir, NodeMgmtDirName)
	ctx := context.Background()
	paths, err := scout.Discover(ctx, createIfNotFound)
	req.NoError(err)
	req.NotEmpty(paths.WorkDir)
	req.NotEmpty(paths.LogDir)
	req.Contains(paths.LogDir, scout.directoryNames.LogDirName)
	req.NotEmpty(paths.ConfigDir)
	req.Contains(paths.ConfigDir, scout.directoryNames.ConfigDirName)
	req.NotEmpty(paths.HederaAppDir)
	req.Equal(hgcappDir, paths.HederaAppDir.Root)
	req.Equal(nmtDir, path.Join(hgcappDir, NodeMgmtDirName), paths.HederaAppDir.NodeMgmtTools.Root)
	assertNodeMgmtPaths(t, hgcappDir, paths.HederaAppDir.NodeMgmtTools)
}

func assertNodeMgmtPaths(t *testing.T, rootDir string, nmtDir *NodeMgmtToolsDir) {
	req := require.New(t)
	nodeMgmtRoot := filepath.Join(rootDir, NodeMgmtDirName)
	req.Equal(nodeMgmtRoot, nmtDir.Root)
	req.Equal(filepath.Join(nodeMgmtRoot, "/bin"), nmtDir.Bin)
	req.Equal(filepath.Join(nodeMgmtRoot, "/common"), nmtDir.Common)
	req.Equal(filepath.Join(nodeMgmtRoot, "/compose"), nmtDir.Compose.Root)
	req.Equal(filepath.Join(nodeMgmtRoot, "/compose/network-node"), nmtDir.Compose.NetworkNode)
	req.Equal(filepath.Join(nodeMgmtRoot, "/config"), nmtDir.Config)
	req.Equal(filepath.Join(nodeMgmtRoot, "/images"), nmtDir.Image)
	req.Equal(filepath.Join(nodeMgmtRoot, "/logs"), nmtDir.Logs)
	req.Equal(filepath.Join(nodeMgmtRoot, "/state"), nmtDir.State)
	assertUpgradePaths(t, nmtDir.Root, nmtDir.Upgrade)
}

func assertUpgradePaths(t *testing.T, rootDir string, upgradeDir *UpgradeDir) {
	req := require.New(t)
	req.Equal(filepath.Join(rootDir, "/upgrade"), upgradeDir.Root)
	req.Equal(filepath.Join(rootDir, "/upgrade/current"), upgradeDir.Current)
	req.Equal(filepath.Join(rootDir, "/upgrade/previous"), upgradeDir.Previous)
	req.Equal(filepath.Join(rootDir, "/upgrade/pending"), upgradeDir.Pending)
}

func assertHapiAppDataPaths(t *testing.T, rootDir string, dataDir *HapiAppDataDir) {
	req := require.New(t)
	dataRoot := filepath.Join(rootDir, DataDirName)
	req.Equal(dataRoot, dataDir.Root)
	req.Equal(filepath.Join(dataRoot, "/config"), dataDir.Config)
	req.Equal(filepath.Join(dataRoot, "/diskFs"), dataDir.DiskFs)
	req.Equal(filepath.Join(dataRoot, "/keys"), dataDir.Keys)
	req.Equal(filepath.Join(dataRoot, "/onboard"), dataDir.OnBoard)
	req.Equal(filepath.Join(dataRoot, "/saved"), dataDir.Saved)
	req.Equal(filepath.Join(dataRoot, "/stats"), dataDir.Stats)
	assertUpgradePaths(t, dataDir.Root, dataDir.Upgrade)
}

func assertHapiAppPaths(t *testing.T, rootDir string, hapiAppDir *HapiAppDir) {
	req := require.New(t)
	hapiAppRoot := filepath.Join(rootDir, HederaApiDirName)
	req.Equal(hapiAppRoot, hapiAppDir.Root)
	assertHapiAppDataPaths(t, hapiAppRoot, hapiAppDir.Data)
}
