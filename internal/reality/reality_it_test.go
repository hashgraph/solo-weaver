// SPDX-License-Identifier: Apache-2.0

//go:build integration_require_block_node_installed

package reality

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/stretchr/testify/require"
)

func TestRealityChecker_BlockNodeState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	current := core.NewState()
	reality, err := NewChecker(current)
	require.NoError(t, err)
	require.NotNil(t, reality)

	st, err := reality.BlockNodeState(ctx)
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
	require.Empty(t, st.ReleaseInfo.Deleted)
	require.True(t, st.ReleaseInfo.Deleted.IsZero())

	require.NotEmpty(t, st.Storage.LivePath)
	require.NotEmpty(t, st.Storage.LiveSize)
	require.NotEmpty(t, st.Storage.ArchivePath)
	require.NotEmpty(t, st.Storage.ArchiveSize)
	require.NotEmpty(t, st.Storage.LogPath)
	require.NotEmpty(t, st.Storage.LogSize)
	require.Empty(t, st.Storage.BasePath) // since specific paths will be set

	require.NotEmpty(t, st.LastSync)
}
