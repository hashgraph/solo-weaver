// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// recordedCluster is a good cluster record a block node install could find on disk.
var recordedCluster = state.ClusterState{
	Created:     true,
	ClusterInfo: models.ClusterInfo{Host: "https://10.0.0.1:6443"},
	Releases: map[string]state.HelmReleaseInfo{
		"metallb": {Name: "metallb", Namespace: "metallb-system", ChartVersion: "0.16.1", Status: release.StatusDeployed},
	},
}

func seedCluster(t *testing.T, stateFile string) {
	t.Helper()
	writeState(t, stateFile, state.TeleportState{})
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	st := sm.State()
	st.ClusterState = recordedCluster
	require.NoError(t, sm.Set(st).Commit(state.ActionHistory{}, state.ComponentCluster))
}

// A handler that owns the cluster record, like a block node install that
// creates the cluster, keeps the record when its flush cannot read the
// cluster, and writes what it reads when it can.
func TestFlushState_ClusterRecordFollowsWhatTheRefreshCouldRead(t *testing.T) {
	for name, tc := range map[string]struct {
		h    *host
		want state.ClusterState
	}{
		"API server silent: the record stays": {
			h:    &host{clusterProbe: func() reality.ClusterReachability { return reality.Unobservable }},
			want: recordedCluster,
		},
		"cluster read: the live cluster is written": {
			h:    &host{},
			want: state.ClusterState{Created: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			stateFile := filepath.Join(t.TempDir(), "state.yaml")
			seedCluster(t, stateFile)

			_, err := handleBlockNodeIntentWith(t, stateFile, tc.h, noopIntent{}, BlockNode, Cluster)
			require.NoError(t, err)

			got := readState(t, stateFile).ClusterState
			require.Equal(t, tc.want.Created, got.Created)
			require.Equal(t, tc.want.Host, got.Host)
			require.Equal(t, tc.want.Releases, got.Releases)
		})
	}
}
