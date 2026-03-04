// SPDX-License-Identifier: Apache-2.0

//go:build integration

package reality

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
)

// newIntegrationStateManager returns a minimal state.Manager backed by a
// temporary in-memory state for use in integration tests.
func newIntegrationStateManager(t *testing.T) state.Manager {
	t.Helper()
	sm, err := state.NewStateManager()
	require.NoError(t, err)
	return sm
}

func TestRealityChecker_BlockNodeState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sm := newIntegrationStateManager(t)
	checker, err := NewChecker(sm)
	require.NoError(t, err)
	require.NotNil(t, checker)

	st, err := checker.BlockNodeState(ctx)
	require.NoError(t, err)
	require.NotNil(t, st)

	require.NotEmpty(t, st.ReleaseInfo)
	require.NotEmpty(t, st.ReleaseInfo.Name)
	require.NotEmpty(t, st.ReleaseInfo.Version)
	require.NotEmpty(t, st.ReleaseInfo.Namespace)
	require.NotEmpty(t, st.ReleaseInfo.ChartName)
	require.NotEmpty(t, st.ReleaseInfo.ChartVersion)
	require.NotEmpty(t, st.ReleaseInfo.Status)
	require.NotEmpty(t, st.ReleaseInfo.FirstDeployed)
	require.NotEmpty(t, st.ReleaseInfo.LastDeployed)
	require.True(t, st.ReleaseInfo.Deleted.IsZero())

	require.NotEmpty(t, st.Storage.LivePath)
	require.NotEmpty(t, st.Storage.LiveSize)
	require.NotEmpty(t, st.Storage.ArchivePath)
	require.NotEmpty(t, st.Storage.ArchiveSize)
	require.NotEmpty(t, st.Storage.LogPath)
	require.NotEmpty(t, st.Storage.LogSize)
	require.Empty(t, st.Storage.BasePath) // specific paths are set instead

	require.NotEmpty(t, st.LastSync)
}

func TestRealityChecker_ClusterState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sm := newIntegrationStateManager(t)
	checker, err := NewChecker(sm)
	require.NoError(t, err)
	require.NotNil(t, checker)

	st, err := checker.ClusterState(ctx)
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestRealityChecker_MachineState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sm := newIntegrationStateManager(t)
	checker, err := NewChecker(sm)
	require.NoError(t, err)
	require.NotNil(t, checker)

	st, err := checker.MachineState(ctx)
	require.NoError(t, err)
	require.NotNil(t, st)

	require.NotEmpty(t, st.Hardware)
	require.NotNil(t, st.Software)
}
