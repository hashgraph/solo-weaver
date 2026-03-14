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

// newTestBlockNodeChecker creates a blockNodeChecker backed by a real cluster probe,
// helm manager, and kube client — i.e. whatever is reachable from the test host.
func newTestBlockNodeChecker(t *testing.T) reality.Checker[state.BlockNodeState] {
	t.Helper()
	sm := newTestStateManager(t)
	checkers, err := reality.NewCheckers(sm)
	require.NoError(t, err)
	return checkers.BlockNode
}

// TestBlockNodeChecker_Integration_RefreshState exercises the live path when a
// cluster IS reachable. The test is lenient: it only asserts that no error is
// returned and that the returned state is structurally consistent.
func TestBlockNodeChecker_Integration_RefreshState(t *testing.T) {
	checker := newTestBlockNodeChecker(t)

	bn, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	// If a BlockNode release was found the ReleaseInfo must be populated.
	if bn.ReleaseInfo.Name != "" {
		t.Run("ReleaseInfo is consistent", func(t *testing.T) {
			assert.NotEmpty(t, bn.ReleaseInfo.Namespace, "Namespace must be set when a release is present")
			assert.NotEmpty(t, bn.ReleaseInfo.ChartName, "ChartName must be set when a release is present")
			assert.NotEmpty(t, bn.ReleaseInfo.ChartVersion, "ChartVersion must be set when a release is present")
			assert.NotEmpty(t, bn.ReleaseInfo.Status, "Status must be set when a release is present")
		})

		t.Run("LastSync is set", func(t *testing.T) {
			assert.False(t, bn.LastSync.IsZero(), "LastSync should be set after RefreshState")
		})
	}
}

// TestBlockNodeChecker_Integration_FlushState_Idempotent verifies that calling
// FlushState twice with the same BlockNodeState does NOT re-write the state file
// on the second call (mtime must not change).
func TestBlockNodeChecker_Integration_FlushState_Idempotent(t *testing.T) {
	sm := newTestStateManager(t)
	checker := newTestBlockNodeChecker(t)

	err := sm.FlushState()
	require.NoError(t, err)

	ctx := context.Background()
	bn, err := checker.RefreshState(ctx)
	require.NoError(t, err)

	info1, err := os.Stat(sm.State().StateFile)
	require.NoError(t, err)

	// Second flush with identical state must not touch the file.
	err = checker.FlushState(bn)
	require.NoError(t, err)

	info2, err := os.Stat(sm.State().StateFile)
	require.NoError(t, err)

	assert.Equal(t, info1.ModTime(), info2.ModTime(),
		"FlushState with identical BlockNodeState must not re-write the state file")
}

// TestBlockNodeChecker_Integration_StatePersistedToDisk verifies that the state
// written by RefreshState can be read back by a fresh state.Manager.
func TestBlockNodeChecker_Integration_StatePersistedToDisk(t *testing.T) {
	sm := newTestStateManager(t)
	checker := newTestBlockNodeChecker(t)

	bn, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	// Re-read from a brand-new manager pointing at the same file.
	sm2, err := state.NewStateManager(state.WithStateFile(sm.State().StateFile))
	require.NoError(t, err)
	require.NoError(t, sm2.Refresh())

	persisted := sm2.State().BlockNodeState

	assert.Equal(t, bn.ReleaseInfo.Name, persisted.ReleaseInfo.Name,
		"persisted ReleaseInfo.Name must match in-memory value")
}
