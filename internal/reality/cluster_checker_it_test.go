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

// newTestClusterChecker creates a clusterChecker backed by a real cluster probe —
// i.e. whatever Kubernetes context is reachable from the test host.
func newTestClusterChecker(t *testing.T) reality.Checker[state.ClusterState] {
	t.Helper()
	checkers, err := reality.NewCheckers(newTestStateManager(t))
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
