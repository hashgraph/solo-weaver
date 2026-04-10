// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

// fakeHelmManager implements reality.HelmManager for testing.
type fakeHelmManager struct {
	releases []*release.Release
}

func (f *fakeHelmManager) ListAll() ([]*release.Release, error) {
	return f.releases, nil
}

func newTeleportTestStateManager(t *testing.T) state.Manager {
	t.Helper()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	sm, err := state.NewStateManager(state.WithStateFile(stateFile))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	require.NoError(t, sm.FlushAll())
	return sm
}

func TestTeleportChecker_ClusterNotExists_SkipsHelmChecks(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	// Cluster probe returns false — Helm should never be called
	clusterExists := func() (bool, error) { return false, nil }
	helmCalled := false
	newHelm := func() (reality.HelmManager, error) {
		helmCalled = true
		return &fakeHelmManager{}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, clusterExists)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, helmCalled, "Helm should not be called when cluster does not exist")
	assert.False(t, ts.ClusterAgent.Installed, "ClusterAgent should not be installed when no cluster")
}

func TestTeleportChecker_HelmReleasePresent_ClusterAgentInstalled(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	clusterExists := func() (bool, error) { return true, nil }
	newHelm := func() (reality.HelmManager, error) {
		return &fakeHelmManager{
			releases: []*release.Release{
				{
					Name:      deps.TELEPORT_RELEASE,
					Namespace: deps.TELEPORT_NAMESPACE,
					Chart: &chart.Chart{
						Metadata: &chart.Metadata{
							Version: "18.6.4",
						},
					},
				},
			},
		}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, clusterExists)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.True(t, ts.ClusterAgent.Installed, "ClusterAgent should be installed when Helm release is present")
	assert.Equal(t, deps.TELEPORT_RELEASE, ts.ClusterAgent.Release)
	assert.Equal(t, deps.TELEPORT_NAMESPACE, ts.ClusterAgent.Namespace)
	assert.Equal(t, "18.6.4", ts.ClusterAgent.ChartVersion)
}

func TestTeleportChecker_HelmReleaseAbsent_ClusterAgentNotInstalled(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	clusterExists := func() (bool, error) { return true, nil }
	newHelm := func() (reality.HelmManager, error) {
		return &fakeHelmManager{releases: nil}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, clusterExists)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, ts.ClusterAgent.Installed, "ClusterAgent should not be installed when no Helm release")
}
