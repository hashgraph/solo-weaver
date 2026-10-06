// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package drift

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func deployedBlockNode() state.BlockNodeState {
	pool := true
	return state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Name: "block-node", ChartVersion: "0.41.0", Namespace: "block-node",
			ChartRef: "oci://ghcr.io/hiero-ledger/hiero-block-node", ChartName: "block-node-server",
			Status: release.StatusDeployed,
		},
		Storage:         models.BlockNodeStorage{LivePath: "/mnt/fast-storage/live", LiveSize: "20Gi"},
		ServiceTopology: state.ServiceTopologySingle,
		MetalLBPool:     &pool,
		PluginPreset:    "minimal",
	}
}

func withBlockNode(b state.BlockNodeState) state.State {
	s := state.State{}
	s.BlockNodeState = b
	return s
}

func TestBlockNode_SameStateHasNoChanges(t *testing.T) {
	require.Empty(t, BlockNode(withBlockNode(deployedBlockNode()), withBlockNode(deployedBlockNode())))
}

// With no release on either side there is no block node to drift, whatever
// the record still holds.
func TestBlockNode_NeitherSideInstalledHasNoChanges(t *testing.T) {
	persisted := state.NewBlockNodeState()
	persisted.Storage.LivePath = "/mnt/fast-storage/live"

	require.Empty(t, BlockNode(withBlockNode(persisted), withBlockNode(state.NewBlockNodeState())))
}

func TestBlockNode_RemovalIsReportedAsTheInstalledFlagOnly(t *testing.T) {
	got := BlockNode(withBlockNode(deployedBlockNode()), withBlockNode(state.NewBlockNodeState()))

	require.Equal(t, []Change{
		{Component: "block node", Field: "installed", Persisted: "true", Live: "false"},
	}, got)
}

func TestBlockNode_InstallIsReportedAsTheInstalledFlagOnly(t *testing.T) {
	got := BlockNode(withBlockNode(state.NewBlockNodeState()), withBlockNode(deployedBlockNode()))

	require.Equal(t, []Change{
		{Component: "block node", Field: "installed", Persisted: "false", Live: "true"},
	}, got)
}

func TestBlockNode_ReportsObservableChangesOnly(t *testing.T) {
	live := deployedBlockNode()
	noPool := false
	live.ReleaseInfo.ChartVersion = "0.42.0"
	live.Storage.LiveSize = "30Gi"
	live.MetalLBPool = &noPool
	// Weaver-only records: reality cannot read them back.
	live.ReleaseInfo.ChartRef = "oci://example.com/other"
	live.PluginPreset = ""

	got := BlockNode(withBlockNode(deployedBlockNode()), withBlockNode(live))

	require.Equal(t, []Change{
		{Component: "block node", Field: "version", Persisted: "0.41.0", Live: "0.42.0"},
		{Component: "block node", Field: "storage.liveSize", Persisted: "20Gi", Live: "30Gi"},
		{Component: "block node", Field: "metallbPool", Persisted: "true", Live: "false"},
	}, got)
}
