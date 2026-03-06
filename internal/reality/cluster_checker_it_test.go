//go:build integration

// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClusterChecker creates a clusterChecker backed by a real cluster probe —
// i.e. whatever Kubernetes context is reachable from the test host.
func newTestClusterChecker(t *testing.T) reality.Checker[state.ClusterState] {
	t.Helper()
	sm := newTestStateManager(t)
	checkers, err := reality.NewCheckers(sm)
	require.NoError(t, err)
	return checkers.Cluster
}

// TestClusterChecker_Integration_RefreshState exercises the live path when a
// cluster IS reachable. The test is lenient: it only asserts that no error is
// returned and that the returned state is structurally consistent.
func TestClusterChecker_Integration_RefreshState(t *testing.T) {
	checker := newTestClusterChecker(t)

	cs, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	if cs.Created {
		t.Run("ClusterInfo is consistent", func(t *testing.T) {
			assert.NotEmpty(t, cs.ClusterInfo, "ClusterInfo must be populated when cluster is created")
		})

		t.Run("LastSync is set", func(t *testing.T) {
			assert.False(t, cs.LastSync.IsZero(), "LastSync must be set after a successful RefreshState")
		})
	}
}

// TestClusterChecker_Integration_FlushState_Idempotent verifies that calling
// FlushState twice with the same ClusterState does NOT re-write the state file
// on the second call (mtime must not change).
func TestClusterChecker_Integration_FlushState_Idempotent(t *testing.T) {
	sm := newTestStateManager(t)

	// Use a probe that always reports "no cluster" for a deterministic, stable state.
	checker, err := reality.NewClusterChecker(sm, func() (bool, error) { return false, nil })
	require.NoError(t, err)

	err = sm.FlushState()
	require.NoError(t, err)

	ctx := context.Background()
	cs, err := checker.RefreshState(ctx)
	require.NoError(t, err)

	info1, err := os.Stat(sm.State().StateFile)
	require.NoError(t, err)

	// Second flush with identical state must not touch the file.
	err = checker.FlushState(cs)
	require.NoError(t, err)

	info2, err := os.Stat(sm.State().StateFile)
	require.NoError(t, err)

	assert.Equal(t, info1.ModTime(), info2.ModTime(),
		"FlushState with identical ClusterState must not re-write the state file")
}

// TestClusterChecker_Integration_StatePersistedToDisk verifies that the state
// written by RefreshState can be read back by a fresh state.Manager.
func TestClusterChecker_Integration_StatePersistedToDisk(t *testing.T) {
	sm := newTestStateManager(t)

	checker, err := reality.NewClusterChecker(sm, func() (bool, error) { return false, nil })
	require.NoError(t, err)

	cs, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	// Re-read from a brand-new manager pointing at the same file.
	sm2, err := state.NewStateManager(state.WithStateFile(sm.State().StateFile))
	require.NoError(t, err)
	require.NoError(t, sm2.Refresh())

	persisted := sm2.State().ClusterState

	assert.Equal(t, cs.Created, persisted.Created,
		"persisted Created must match in-memory value")
	assert.True(t, cs.ClusterInfo.Equal(persisted.ClusterInfo),
		"persisted ClusterInfo must match in-memory value")
}
