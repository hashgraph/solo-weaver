// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// mustStateWriter creates a DefaultStateManager for use in integration tests.
// It refreshes from disk so any previously persisted state is loaded;
// a missing state file (fresh environment) is silently ignored.
func mustStateWriter(t *testing.T) state.Manager {
	t.Helper()
	sm, err := state.NewStateManager()
	require.NoError(t, err, "failed to create state manager for test")
	if err = sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		require.NoError(t, err, "failed to refresh state manager for test")
	}
	return sm
}
