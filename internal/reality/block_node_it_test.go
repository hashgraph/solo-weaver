//go:build integration

// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
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
