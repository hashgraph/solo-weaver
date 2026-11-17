// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

// liveInfo is the cluster info a readable cluster reports in these tests.
var liveInfo = &models.ClusterInfo{Host: "https://10.0.0.9:6443"}

func helmRelease(name, namespace, chartName, version string, status release.Status) *release.Release {
	return &release.Release{
		Name:      name,
		Namespace: namespace,
		Info:      &release.Info{Status: status},
		Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: chartName, Version: version, AppVersion: "v" + version}},
	}
}

// catalogRelease is a release of the catalog chart name at version.
func catalogRelease(name, version string, status release.Status) *release.Release {
	ch := software.MustGetClusterComponent(name)
	return helmRelease(ch.Release, ch.Namespace, name, version, status)
}

// smWithCluster is a state manager whose persisted cluster is cs.
func smWithCluster(t *testing.T, cs state.ClusterState) state.Manager {
	t.Helper()
	sm := newTeleportTestStateManager(t)
	st := sm.State()
	st.ClusterState = cs
	sm.Set(st)
	return sm
}

// smWithReleases is a state manager whose persisted cluster records releases.
func smWithReleases(t *testing.T, releases map[string]state.HelmReleaseInfo) state.Manager {
	return smWithCluster(t, state.ClusterState{Releases: releases})
}

func refreshCluster(t *testing.T, sm state.Manager, helm reality.HelmManager, probe reality.ClusterReachability) state.ClusterState {
	t.Helper()
	reality.UseClusterInfo(t, liveInfo, nil)
	return refreshClusterAsIs(t, sm, helm, probe)
}

// refreshClusterAsIs refreshes with whatever cluster info the test set up.
func refreshClusterAsIs(t *testing.T, sm state.Manager, helm reality.HelmManager, probe reality.ClusterReachability) state.ClusterState {
	t.Helper()
	checker, err := reality.NewClusterChecker(sm,
		func() (reality.HelmManager, error) { return helm, nil },
		func() reality.ClusterReachability { return probe })
	require.NoError(t, err)
	cs, err := checker.RefreshState(context.Background())
	require.NoError(t, err)
	return cs
}

var recorded = map[string]state.HelmReleaseInfo{
	"metallb": {Name: "metallb", Namespace: "metallb-system", ChartVersion: "0.15.2", Status: release.StatusDeployed},
}

func TestClusterChecker_RecordsManagedReleases(t *testing.T) {
	metallb := software.MustGetClusterComponent("metallb")
	helm := &fakeHelmManager{releases: []*release.Release{
		catalogRelease("metallb", "0.16.1", release.StatusDeployed),
		catalogRelease("alloy", "1.13.1", release.StatusFailed),
		helmRelease("cilium", "kube-system", "cilium", "1.20.2", release.StatusDeployed),
		// Recorded by other components, so left out.
		catalogRelease("teleport-cluster-agent", "18.11.3", release.StatusDeployed),
		catalogRelease("solo-operator", "0.11.0", release.StatusDeployed),
		// Not a managed release: same name, other namespace.
		helmRelease(metallb.Release, "default", "metallb", "0.1.0", release.StatusDeployed),
	}}

	cs := refreshCluster(t, smWithReleases(t, nil), helm, reality.Reachable)

	require.Equal(t, map[string]state.HelmReleaseInfo{
		"metallb": {
			Name: metallb.Release, Namespace: metallb.Namespace, ChartRef: metallb.Chart, ChartName: "metallb",
			ChartVersion: "0.16.1", AppVersion: "v0.16.1", Status: release.StatusDeployed,
		},
		"alloy": {
			Name: software.MustGetClusterComponent("alloy").Release, Namespace: software.MustGetClusterComponent("alloy").Namespace,
			ChartRef: software.MustGetClusterComponent("alloy").Chart, ChartName: "alloy",
			ChartVersion: "1.13.1", AppVersion: "v1.13.1", Status: release.StatusFailed,
		},
		reality.CiliumReleaseKey: {
			Name: "cilium", Namespace: "kube-system", ChartName: "cilium",
			ChartVersion: "1.20.2", AppVersion: "v1.20.2", Status: release.StatusDeployed,
		},
	}, cs.Releases)
}

// A reachable cluster with no managed release clears what was recorded.
func TestClusterChecker_NoReleasesClearsTheRecord(t *testing.T) {
	cs := refreshCluster(t, smWithReleases(t, recorded), &fakeHelmManager{}, reality.Reachable)

	require.Empty(t, cs.Releases)
}

// A refresh that cannot read the whole cluster returns the recorded cluster
// unchanged, so a commit after it keeps the record. It is marked Unobserved
// only when a kubeconfig exists.
func TestClusterChecker_KeepsTheRecordedClusterWhenNotObserved(t *testing.T) {
	persisted := state.ClusterState{
		Created:     true,
		ClusterInfo: models.ClusterInfo{Host: "https://10.0.0.1:6443"},
		Releases:    recorded,
	}
	for name, tc := range map[string]struct {
		helm           reality.HelmManager
		probe          reality.ClusterReachability
		infoErr        error
		wantUnobserved bool
	}{
		"no kubeconfig":           {helm: &fakeHelmManager{}, probe: reality.NotConfigured},
		"API server silent":       {helm: &fakeHelmManager{}, probe: reality.Unobservable, wantUnobserved: true},
		"helm listing failed":     {helm: failingHelmManager{}, probe: reality.Reachable, wantUnobserved: true},
		"cluster info unreadable": {helm: &fakeHelmManager{}, probe: reality.Reachable, infoErr: errors.New("connection refused"), wantUnobserved: true},
	} {
		t.Run(name, func(t *testing.T) {
			reality.UseClusterInfo(t, liveInfo, tc.infoErr)

			cs := refreshClusterAsIs(t, smWithCluster(t, persisted), tc.helm, tc.probe)

			require.Equal(t, tc.wantUnobserved, cs.Unobserved)
			cs.Unobserved = false
			require.Equal(t, persisted, cs)
		})
	}
}

// A cluster read in full replaces the record, including a cluster recorded as
// never created.
func TestClusterChecker_ObservedClusterReplacesTheRecord(t *testing.T) {
	cs := refreshCluster(t, smWithCluster(t, state.ClusterState{Releases: recorded}),
		&fakeHelmManager{releases: []*release.Release{catalogRelease("metallb", "0.16.1", release.StatusDeployed)}},
		reality.Reachable)

	require.False(t, cs.Unobserved)
	require.True(t, cs.Created)
	require.Equal(t, liveInfo.Host, cs.Host)
	require.Equal(t, "0.16.1", cs.Releases["metallb"].ChartVersion)
}

// A chart installed outside its catalog namespace is found by release and
// chart name, as long as there is only one such release.
func TestClusterChecker_FindsAReleaseInAnotherNamespace(t *testing.T) {
	eso := software.MustGetClusterComponent("external-secrets")
	elsewhere := helmRelease(eso.Release, "my-eso", "external-secrets", "2.13.0", release.StatusDeployed)

	cs := refreshCluster(t, smWithReleases(t, nil), &fakeHelmManager{releases: []*release.Release{elsewhere}}, reality.Reachable)
	require.Equal(t, "my-eso", cs.Releases["external-secrets"].Namespace)

	twice := []*release.Release{elsewhere, helmRelease(eso.Release, "other-eso", "external-secrets", "2.13.0", release.StatusDeployed)}
	cs = refreshCluster(t, smWithReleases(t, nil), &fakeHelmManager{releases: twice}, reality.Reachable)
	require.NotContains(t, cs.Releases, "external-secrets")

	otherChart := helmRelease(eso.Release, "my-eso", "something-else", "1.0.0", release.StatusDeployed)
	cs = refreshCluster(t, smWithReleases(t, nil), &fakeHelmManager{releases: []*release.Release{otherChart}}, reality.Reachable)
	require.NotContains(t, cs.Releases, "external-secrets")
}
