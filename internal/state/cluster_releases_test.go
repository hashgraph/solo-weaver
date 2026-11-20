// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func commitCluster(t *testing.T, dir string, cs ClusterState) {
	t.Helper()
	sm, err := NewStateManager(WithStateFile(filepath.Join(dir, StateFileName)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	st := sm.State()
	st.ClusterState = cs
	require.NoError(t, sm.Set(st).Commit(ActionHistory{}, ComponentCluster))
}

func loadCluster(t *testing.T, dir string) ClusterState {
	t.Helper()
	sm, err := NewStateManager(WithStateFile(filepath.Join(dir, StateFileName)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm.State().ClusterState
}

func TestClusterReleases_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	releases := map[string]HelmReleaseInfo{
		"metallb": {Name: "metallb", Namespace: "metallb-system", ChartRef: "metallb/metallb", ChartName: "metallb",
			ChartVersion: "0.16.1", AppVersion: "v0.16.1", Status: release.StatusDeployed},
		"cilium": {Name: "cilium", Namespace: "kube-system", ChartName: "cilium", ChartVersion: "1.20.2", Status: release.StatusDeployed},
	}

	commitCluster(t, dir, ClusterState{Created: true, Releases: releases})

	require.Equal(t, releases, loadCluster(t, dir).Releases)
}

// A cluster file written before Releases existed has no releases key and
// loads as nothing recorded.
func TestClusterReleases_OlderFileLoadsAsNotRecorded(t *testing.T) {
	dir := t.TempDir()
	commitCluster(t, dir, ClusterState{Created: true})

	raw, err := os.ReadFile(ComponentFilePath(dir, ComponentCluster))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "releases")

	got := loadCluster(t, dir)
	require.True(t, got.Created)
	require.Nil(t, got.Releases)
}

func TestClusterState_CloneCopiesReleases(t *testing.T) {
	cs := ClusterState{Releases: map[string]HelmReleaseInfo{"metallb": {ChartVersion: "0.16.1"}}}

	clone, err := cs.Clone()
	require.NoError(t, err)
	clone.Releases["metallb"] = HelmReleaseInfo{ChartVersion: "0.15.2"}

	require.Equal(t, "0.16.1", cs.Releases["metallb"].ChartVersion)
}

func TestClusterState_EqualComparesReleases(t *testing.T) {
	a := ClusterState{Releases: map[string]HelmReleaseInfo{"metallb": {ChartVersion: "0.16.1"}}}
	b := ClusterState{Releases: map[string]HelmReleaseInfo{"metallb": {ChartVersion: "0.15.2"}}}

	require.True(t, a.Equal(a))
	require.False(t, a.Equal(b))
	require.False(t, a.Equal(ClusterState{}))
}
