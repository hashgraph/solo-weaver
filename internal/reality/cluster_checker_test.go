// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/cilium"
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

// fakeReader is a cluster with nodes and a Cilium agent, or whose reads fail.
type fakeReader struct {
	nodes      []reality.NodeInfo
	agent      string
	agentFound bool
	nodesErr   error
	agentErr   error
}

func (f fakeReader) Nodes(context.Context) ([]reality.NodeInfo, error) { return f.nodes, f.nodesErr }
func (f fakeReader) CiliumAgentVersion(context.Context) (string, bool, error) {
	return f.agent, f.agentFound, f.agentErr
}

var oneNode = fakeReader{
	nodes:      []reality.NodeInfo{{Name: "n1", KubeletVersion: "v1.36.5", ContainerRuntimeVersion: "cri-o://1.36.7"}},
	agent:      "1.20.2",
	agentFound: true,
}

func refreshCluster(t *testing.T, sm state.Manager, helm reality.HelmManager, probe reality.ClusterReachability) state.ClusterState {
	t.Helper()
	return refreshClusterWith(t, sm, helm, oneNode, probe)
}

func refreshClusterWith(t *testing.T, sm state.Manager, helm reality.HelmManager, reader reality.ClusterReader, probe reality.ClusterReachability) state.ClusterState {
	t.Helper()
	noClusterInfo(t)
	checker, err := reality.NewClusterChecker(sm,
		func() (reality.HelmManager, error) { return helm, nil },
		func() (reality.ClusterReader, error) { return reader, nil },
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

func TestClusterChecker_RecordsTheNodeAndTheCiliumAgent(t *testing.T) {
	cs := refreshCluster(t, smWithReleases(t, nil), &fakeHelmManager{}, reality.Reachable)

	require.Equal(t, "1.36.5", cs.KubeletVersion)
	require.Equal(t, "cri-o://1.36.7", cs.ContainerRuntimeVersion)
	require.Equal(t, 1, cs.NodeCount)
	require.Equal(t, "1.20.2", cs.CiliumAgentVersion)
}

// A cluster with no Cilium agent records none; a node joined by hand shows in
// the count.
func TestClusterChecker_NoAgentAndExtraNodes(t *testing.T) {
	reader := fakeReader{nodes: []reality.NodeInfo{
		{Name: "a", KubeletVersion: "v1.36.5"}, {Name: "b", KubeletVersion: "v1.35.2"},
	}}
	sm := newTeleportTestStateManager(t)
	st := sm.State()
	st.ClusterState.CiliumAgentVersion = "1.18.1"
	sm.Set(st)

	cs := refreshClusterWith(t, sm, &fakeHelmManager{}, reader, reality.Reachable)

	require.Equal(t, 2, cs.NodeCount)
	require.Empty(t, cs.CiliumAgentVersion)
}

// A node or agent read that fails keeps the recorded values and marks the
// result unobserved.
func TestClusterChecker_FailedReadKeepsTheRecordedNodeAndAgent(t *testing.T) {
	sm := newTeleportTestStateManager(t)
	st := sm.State()
	st.ClusterState.KubeletVersion = "1.33.4"
	st.ClusterState.NodeCount = 1
	st.ClusterState.CiliumAgentVersion = "1.18.1"
	sm.Set(st)
	failing := fakeReader{nodesErr: errors.New("forbidden"), agentErr: errors.New("timeout")}

	cs := refreshClusterWith(t, sm, &fakeHelmManager{}, failing, reality.Reachable)

	require.Equal(t, "1.33.4", cs.KubeletVersion)
	require.Equal(t, 1, cs.NodeCount)
	require.Equal(t, "1.18.1", cs.CiliumAgentVersion)
	require.True(t, cs.Unobserved)
}

// A release of another chart under a managed release name (the Bitnami
// node-exporter) is recorded with its own chart name and no catalog chartRef.
func TestClusterChecker_ForeignChartGetsNoCatalogChartRef(t *testing.T) {
	ne := software.MustGetClusterComponent("node-exporter")
	bitnami := helmRelease(ne.Release, ne.Namespace, "node-exporter", "4.5.19", release.StatusDeployed)

	cs := refreshCluster(t, smWithReleases(t, nil), &fakeHelmManager{releases: []*release.Release{bitnami}}, reality.Reachable)

	got := cs.Releases["node-exporter"]
	require.Equal(t, "node-exporter", got.ChartName)
	require.Equal(t, "4.5.19", got.ChartVersion)
	require.Empty(t, got.ChartRef)
}

// Cilium is a catalog chart now, so it must be listed once, under the key the
// recorded state already uses, and with no chart ref: it is installed from a
// local chart directory.
func TestManagedReleases_ListsCiliumOnce(t *testing.T) {
	managed, err := reality.ManagedReleases()
	require.NoError(t, err)

	var found []reality.ManagedRelease
	for _, m := range managed {
		if m.Key == reality.CiliumReleaseKey {
			found = append(found, m)
		}
	}
	require.Equal(t, []reality.ManagedRelease{{
		Key:       "cilium",
		Release:   cilium.ReleaseName,
		Namespace: cilium.Namespace,
		Chart:     "cilium",
	}}, found)
}
