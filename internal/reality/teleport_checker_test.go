// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

func teleportCatalogChart() *software.ChartMetadata {
	return software.MustGetClusterComponent("teleport-cluster-agent")
}

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
	probe := func() reality.ClusterReachability { return reality.NotConfigured }
	helmCalled := false
	newHelm := func() (reality.HelmManager, error) {
		helmCalled = true
		return &fakeHelmManager{}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, helmCalled, "Helm should not be called when cluster does not exist")
	assert.False(t, ts.ClusterAgent.Installed, "ClusterAgent should not be installed when no cluster")
}

func TestTeleportChecker_HelmReleasePresent_ClusterAgentInstalled(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	probe := func() reality.ClusterReachability { return reality.Reachable }
	teleport := teleportCatalogChart()
	newHelm := func() (reality.HelmManager, error) {
		return &fakeHelmManager{
			releases: []*release.Release{
				{
					Name:      teleport.Release,
					Namespace: teleport.Namespace,
					Info:      &release.Info{Status: release.StatusDeployed},
					Chart: &chart.Chart{
						Metadata: &chart.Metadata{
							Version: "18.6.4",
						},
					},
				},
			},
		}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.True(t, ts.ClusterAgent.Installed, "ClusterAgent should be installed when Helm release is present")
	assert.Equal(t, teleport.Release, ts.ClusterAgent.Release)
	assert.Equal(t, teleport.Namespace, ts.ClusterAgent.Namespace)
	assert.Equal(t, "18.6.4", ts.ClusterAgent.ChartVersion)
}

// ListAll reports releases in every state, so a release that is not deployed must
// not be read as a live agent.
func TestTeleportChecker_HelmReleaseNotDeployed_ClusterAgentNotInstalled(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	probe := func() reality.ClusterReachability { return reality.Reachable }
	teleport := teleportCatalogChart()
	newHelm := func() (reality.HelmManager, error) {
		return &fakeHelmManager{
			releases: []*release.Release{
				{
					Name:      teleport.Release,
					Namespace: teleport.Namespace,
					Info:      &release.Info{Status: release.StatusFailed},
					Chart:     &chart.Chart{Metadata: &chart.Metadata{Version: "18.6.4"}},
				},
			},
		}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, ts.ClusterAgent.Installed, "a failed release is not a live cluster agent")
}

func TestTeleportChecker_HelmReleaseWithoutChartMetadata_ReportsEmptyVersion(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	probe := func() reality.ClusterReachability { return reality.Reachable }
	teleport := teleportCatalogChart()
	newHelm := func() (reality.HelmManager, error) {
		return &fakeHelmManager{
			releases: []*release.Release{
				{
					Name:      teleport.Release,
					Namespace: teleport.Namespace,
					Info:      &release.Info{Status: release.StatusDeployed},
				},
			},
		}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.True(t, ts.ClusterAgent.Installed)
	assert.Empty(t, ts.ClusterAgent.ChartVersion)
}

func TestTeleportChecker_HelmReleaseAbsent_ClusterAgentNotInstalled(t *testing.T) {
	sm := newTeleportTestStateManager(t)

	probe := func() reality.ClusterReachability { return reality.Reachable }
	newHelm := func() (reality.HelmManager, error) {
		return &fakeHelmManager{releases: nil}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, ts.ClusterAgent.Installed, "ClusterAgent should not be installed when no Helm release")
}

func persistInstalledClusterAgent(t *testing.T, sm state.Manager) state.TeleportClusterAgentState {
	t.Helper()
	agent := state.TeleportClusterAgentState{
		Installed: true, Release: "teleport-agent", Namespace: "teleport-agent", ChartVersion: "18.6.4",
	}
	s := sm.State()
	s.TeleportState.ClusterAgent = agent
	sm.Set(s)
	return agent
}

// A configured cluster that does not answer must not record the cluster agent
// as removed.
func TestTeleportChecker_ClusterUnobservable_KeepsPersistedClusterAgent(t *testing.T) {
	sm := newTeleportTestStateManager(t)
	persisted := persistInstalledClusterAgent(t, sm)

	probe := func() reality.ClusterReachability { return reality.Unobservable }
	helmCalled := false
	newHelm := func() (reality.HelmManager, error) {
		helmCalled = true
		return &fakeHelmManager{}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, helmCalled)
	assert.Equal(t, persisted, ts.ClusterAgent)
}

// No kubeconfig on this host says nothing about the Helm release, so the
// recorded agent stands.
func TestTeleportChecker_NoKubeconfig_KeepsPersistedClusterAgent(t *testing.T) {
	sm := newTeleportTestStateManager(t)
	persisted := persistInstalledClusterAgent(t, sm)

	probe := func() reality.ClusterReachability { return reality.NotConfigured }
	helmCalled := false
	newHelm := func() (reality.HelmManager, error) {
		helmCalled = true
		return &fakeHelmManager{}, nil
	}

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.False(t, helmCalled)
	assert.Equal(t, persisted, ts.ClusterAgent)
}

// Only a reachable cluster whose Helm releases lack the agent confirms removal.
func TestTeleportChecker_ReachableClusterWithoutRelease_ClearsPersistedClusterAgent(t *testing.T) {
	sm := newTeleportTestStateManager(t)
	persistInstalledClusterAgent(t, sm)

	probe := func() reality.ClusterReachability { return reality.Reachable }
	newHelm := func() (reality.HelmManager, error) { return &fakeHelmManager{releases: nil}, nil }

	checker, err := reality.NewTeleportChecker(sm, newHelm, probe)
	require.NoError(t, err)

	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	assert.Equal(t, state.TeleportClusterAgentState{}, ts.ClusterAgent)
}
