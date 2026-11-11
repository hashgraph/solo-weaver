// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

// noClusterInfo makes kube.RetrieveClusterInfo fail fast, so these tests cover
// the releases alone.
func noClusterInfo(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
}

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

// smWithReleases is a state manager whose persisted cluster records releases.
func smWithReleases(t *testing.T, releases map[string]state.HelmReleaseInfo) state.Manager {
	t.Helper()
	sm := newTeleportTestStateManager(t)
	st := sm.State()
	st.ClusterState.Releases = releases
	sm.Set(st)
	return sm
}

func refreshCluster(t *testing.T, sm state.Manager, helm reality.HelmManager, probe reality.ClusterReachability) state.ClusterState {
	t.Helper()
	noClusterInfo(t)
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

// What could not be observed keeps the recorded releases, so a commit after
// the refresh does not save them as gone.
func TestClusterChecker_KeepsRecordedReleasesWhenNotObserved(t *testing.T) {
	for name, tc := range map[string]struct {
		helm  reality.HelmManager
		probe reality.ClusterReachability
	}{
		"no kubeconfig":       {helm: &fakeHelmManager{}, probe: reality.NotConfigured},
		"API server silent":   {helm: &fakeHelmManager{}, probe: reality.Unobservable},
		"helm listing failed": {helm: failingHelmManager{}, probe: reality.Reachable},
	} {
		t.Run(name, func(t *testing.T) {
			cs := refreshCluster(t, smWithReleases(t, recorded), tc.helm, tc.probe)

			require.Equal(t, recorded, cs.Releases)
			require.True(t, cs.Unobserved)
		})
	}
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
