//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/require"
)

// TestWithCluster_ClusterShape reads the shape of the live cluster a
// provisioner install created: one node, Kubernetes and Cilium agreeing with
// themselves, and every installed managed chart current at its catalog
// default.
func TestWithCluster_ClusterShape(t *testing.T) {
	sm, err := state.NewStateManager(state.WithStateFile(filepath.Join(t.TempDir(), state.StateFileName)))
	require.NoError(t, err)
	checkers, err := reality.NewCheckers(sm)
	require.NoError(t, err)
	cs, err := checkers.Cluster.RefreshState(context.Background())
	require.NoError(t, err)
	managed, err := reality.ManagedReleases()
	require.NoError(t, err)

	live := sm.State()
	live.ClusterState = cs
	shape := ResolveClusterShape(live, nil, managed)
	t.Logf("shape: %+v", shape)

	require.True(t, shape.Observed)
	k := shape.Kubernetes
	require.NotEmpty(t, k.ControlPlane)
	require.Equal(t, k.ControlPlane, k.Kubelet)
	require.Equal(t, 1, k.NodeCount)
	require.False(t, k.UnexpectedNodes)
	require.NotEmpty(t, shape.Cilium.Chart)
	require.False(t, shape.Cilium.Mismatch, "agent %s, chart %s", shape.Cilium.Agent, shape.Cilium.Chart)

	for key, chart := range shape.Charts {
		if chart.Missing {
			continue
		}
		require.Equal(t, IdentityCurrent, chart.Identity, "%s: %s", key, chart.Reason)
		want, err := software.MustGetClusterComponent(key).GetDefaultVersion()
		require.NoError(t, err)
		require.Equal(t, want, chart.Live, key)
	}
}
