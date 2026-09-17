// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// planInputs returns the minimal inputs planStorage reads: the requested storage
// and the chart version the paths are resolved against.
func planInputs(basePath string) models.BlockNodeInputs {
	return models.BlockNodeInputs{
		ChartVersion: "0.30.0",
		Storage:      models.BlockNodeStorage{BasePath: basePath},
	}
}

// TestPlanStorage_WipesDeployedPaths covers the property every caller depends on:
// the purge steps are handed the deployed storage, not the requested one. Wiping
// the requested paths would clear empty directories and orphan the live data.
func TestPlanStorage_WipesDeployedPaths(t *testing.T) {
	for _, tc := range []struct {
		name         string
		purgeStorage bool
		requested    string
	}{
		{name: "purge_storage_moving_paths", purgeStorage: true, requested: "/mnt/new"},
		{name: "no_flag_same_paths", purgeStorage: false, requested: "/mnt/storage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ins := planInputs(tc.requested)
			ins.PurgeStorage = tc.purgeStorage

			plan, err := planStorage(deployedBlockNodeState("/mnt/storage"), ins)

			require.NoError(t, err)
			assert.Equal(t, "/mnt/storage", plan.purgeIns.Storage.BasePath,
				"purge steps must target the deployed paths")
			assert.Equal(t, tc.purgeStorage, plan.recreate)
		})
	}
}

// TestPlanStorage_PathChangeWithoutPurgeIsRejected is the guard that makes
// --purge-storage the only way to move a block node: a local PV's hostPath is
// immutable, so a silent path change would leave the PVs bound to the old
// directories.
func TestPlanStorage_PathChangeWithoutPurgeIsRejected(t *testing.T) {
	_, err := planStorage(deployedBlockNodeState("/mnt/storage"), planInputs("/mnt/new"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestPlanStorage_NotDeployedSkipsComparison keeps --force usable on a host with
// no deployed release: there is no deployed storage to compare against, and
// ResolveStoragePaths rejects the empty configuration outright.
func TestPlanStorage_NotDeployedSkipsComparison(t *testing.T) {
	notDeployed := state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{Status: release.StatusUninstalled},
			},
		},
	}

	plan, err := planStorage(notDeployed, planInputs("/mnt/new"))

	require.NoError(t, err)
	assert.Equal(t, "/mnt/new", plan.purgeIns.Storage.BasePath,
		"with nothing deployed the requested paths are the only ones that exist")
	assert.False(t, plan.recreate)
}

// deployedStateAt returns a deployed state at the given chart version carrying
// the given storage record.
func deployedStateAt(chartVersion string, storage models.BlockNodeStorage) state.State {
	return state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{
					Status:       release.StatusDeployed,
					ChartVersion: chartVersion,
				},
				Storage: storage,
			},
		},
	}
}

// TestPlanStorage_UpgradeAcrossOptionalStorageBoundary is the regression test for
// the UAT failure this guard first shipped with: a plain upgrade from 0.30.0 to
// 0.37.0 was rejected outright.
//
// The deployed record holds individual paths and no base path, and 0.37.0 adds
// application-state. Canonicalizing that record at the *requested* version asks
// it for a path it has never had and fails; at its own version it resolves
// cleanly, and application-state is skipped because it has no deployed path to
// move.
func TestPlanStorage_UpgradeAcrossOptionalStorageBoundary(t *testing.T) {
	deployed := models.BlockNodeStorage{
		ArchivePath:      "/mnt/storage/archive",
		LivePath:         "/mnt/storage/live",
		LogPath:          "/mnt/storage/logs",
		VerificationPath: "/mnt/storage/verification",
		PluginsPath:      "/mnt/storage/plugins",
	}

	ins := models.BlockNodeInputs{
		ChartVersion: "0.37.0",
		Storage:      models.BlockNodeStorage{BasePath: "/mnt/storage"},
	}

	plan, err := planStorage(deployedStateAt("0.30.0", deployed), ins)

	require.NoError(t, err, "an upgrade that adds an optional storage is not a path change")
	assert.False(t, plan.recreate)
	assert.Equal(t, "/mnt/storage/archive", plan.purgeIns.Storage.ArchivePath)
	assert.Equal(t, "0.30.0", plan.purgeIns.ChartVersion,
		"the purge steps must resolve the deployed record at the version that wrote it")
}

// TestPlanStorage_MovedPathAcrossVersionsIsStillRejected keeps the guard honest
// across the same version boundary: comparing at each side's own version must
// still catch a core path that actually moves.
func TestPlanStorage_MovedPathAcrossVersionsIsStillRejected(t *testing.T) {
	deployed := models.BlockNodeStorage{
		ArchivePath:      "/mnt/storage/archive",
		LivePath:         "/mnt/storage/live",
		LogPath:          "/mnt/storage/logs",
		VerificationPath: "/mnt/storage/verification",
		PluginsPath:      "/mnt/storage/plugins",
	}

	ins := models.BlockNodeInputs{
		ChartVersion: "0.37.0",
		Storage:      models.BlockNodeStorage{BasePath: "/mnt/new"},
	}

	_, err := planStorage(deployedStateAt("0.30.0", deployed), ins)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestPlanStorage_SharedOptionalPathMoveIsRejected covers the optional storages
// that exist on both sides: plugins spans 0.30.0 and 0.37.0, so moving only that
// directory is a move the guard has to catch even though the core paths match.
func TestPlanStorage_SharedOptionalPathMoveIsRejected(t *testing.T) {
	deployed := models.BlockNodeStorage{
		ArchivePath:      "/mnt/storage/archive",
		LivePath:         "/mnt/storage/live",
		LogPath:          "/mnt/storage/logs",
		VerificationPath: "/mnt/storage/verification",
		PluginsPath:      "/mnt/storage/plugins",
	}

	ins := models.BlockNodeInputs{
		ChartVersion: "0.37.0",
		Storage: models.BlockNodeStorage{
			ArchivePath: "/mnt/storage/archive",
			LivePath:    "/mnt/storage/live",
			LogPath:     "/mnt/storage/logs",
			PluginsPath: "/mnt/elsewhere/plugins",
			// application-state is new at 0.37.0; base path fills it in.
			BasePath: "/mnt/storage",
		},
	}

	_, err := planStorage(deployedStateAt("0.30.0", deployed), ins)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage paths have changed")
}

// TestPlanStorage_UnresolvableDeployedRecordProceeds covers a deployed record
// that cannot be canonicalized at all — no base path and a missing core path.
// There is nothing to compare, so the operation proceeds with the requested
// paths instead of failing on path math.
func TestPlanStorage_UnresolvableDeployedRecordProceeds(t *testing.T) {
	deployed := models.BlockNodeStorage{
		ArchivePath: "/mnt/storage/archive",
		// LivePath and LogPath missing, no BasePath to derive them from.
	}

	ins := models.BlockNodeInputs{
		ChartVersion: "0.37.0",
		Storage:      models.BlockNodeStorage{BasePath: "/mnt/storage"},
	}

	plan, err := planStorage(deployedStateAt("0.30.0", deployed), ins)

	require.NoError(t, err)
	assert.False(t, plan.recreate)
	assert.Equal(t, "/mnt/storage", plan.purgeIns.Storage.BasePath,
		"with no comparable deployed record the requested paths are the only usable ones")
}
