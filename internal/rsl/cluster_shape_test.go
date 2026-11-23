// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	k8sversion "k8s.io/apimachinery/pkg/version"
)

var shapeManaged = []reality.ManagedRelease{
	{Key: "metallb", Release: "metallb", Namespace: "metallb-system", Chart: "metallb"},
	{Key: "external-secrets", Release: "external-secrets", Namespace: "external-secrets", Chart: "external-secrets"},
	{Key: "node-exporter", Release: "node-exporter", Namespace: "node-exporter", Chart: "prometheus-node-exporter"},
	{Key: "alloy", Release: "grafana-alloy", Namespace: "grafana-alloy", Chart: "alloy"},
	{Key: reality.CiliumReleaseKey, Release: "cilium", Namespace: "kube-system", Chart: "cilium"},
}

func rel(chart, version string) state.HelmReleaseInfo {
	return state.HelmReleaseInfo{ChartName: chart, ChartVersion: version, Status: release.StatusDeployed}
}

// liveCluster is a cluster on the catalog versions: k8s 1.36.5, Cilium 1.20.2,
// every chart current except alloy, which is not installed.
func liveCluster() state.State {
	st := state.NewState("")
	st.ClusterState = state.ClusterState{
		Created:                 true,
		KubeletVersion:          "1.36.5",
		ContainerRuntimeVersion: "cri-o://1.36.7",
		NodeCount:               1,
		CiliumAgentVersion:      "1.20.2",
		Releases: map[string]state.HelmReleaseInfo{
			"metallb":          rel("metallb", "0.16.1"),
			"external-secrets": rel("external-secrets", "2.13.0"),
			"node-exporter":    rel("prometheus-node-exporter", "4.59.0"),
			"cilium":           rel("cilium", "1.20.2"),
		},
	}
	st.ClusterState.ServerVersion = k8sversion.Info{GitVersion: "v1.36.5"}
	st.MachineState.Software = map[string]state.SoftwareState{
		"kubelet": {Name: "kubelet", Version: "1.36.5", Installed: true},
		"kubeadm": {Name: "kubeadm", Version: "1.36.5", Installed: true},
	}
	return st
}

func TestResolveClusterShape_CurrentCluster(t *testing.T) {
	live := liveCluster()
	recorded := liveCluster()

	shape := ResolveClusterShape(live, &recorded, shapeManaged)

	require.True(t, shape.Observed)
	require.Equal(t, KubernetesShape{
		ControlPlane: "1.36.5", Kubelet: "1.36.5", KubeletBinary: "1.36.5", Kubeadm: "1.36.5",
		ContainerRuntime: "cri-o://1.36.7", NodeCount: 1,
	}, shape.Kubernetes)
	require.Equal(t, CiliumShape{Chart: "1.20.2", Agent: "1.20.2", Recorded: "1.20.2"}, shape.Cilium)
	require.Equal(t, ChartShape{Live: "0.16.1", ChartName: "metallb", Status: release.StatusDeployed,
		Identity: IdentityCurrent, Recorded: "0.16.1"}, shape.Charts["metallb"])
	require.Equal(t, IdentityCurrent, shape.Charts["external-secrets"].Identity)
	require.Equal(t, IdentityCurrent, shape.Charts["node-exporter"].Identity)
	require.Equal(t, ChartShape{Missing: true}, shape.Charts["alloy"])
	require.NotContains(t, shape.Charts, reality.CiliumReleaseKey)
}

func TestResolveClusterShape_Identity(t *testing.T) {
	for name, tc := range map[string]struct {
		key      string
		release  state.HelmReleaseInfo
		identity Identity
		reason   string
	}{
		"Bitnami node-exporter": {"node-exporter", rel("node-exporter", "4.5.19"), IdentityLegacy,
			"chart node-exporter, the catalog manages prometheus-node-exporter"},
		"ESO 0.x":                {"external-secrets", rel("external-secrets", "0.19.2"), IdentityLegacy, "chart major 0, the catalog manages 2 and later"},
		"ESO 1.x":                {"external-secrets", rel("external-secrets", "1.3.1"), IdentityLegacy, "chart major 1, the catalog manages 2 and later"},
		"ESO 2.x":                {"external-secrets", rel("external-secrets", "v2.13.0"), IdentityCurrent, ""},
		"ESO unreadable":         {"external-secrets", rel("external-secrets", "latest"), IdentityUnknown, `chart version "latest" is not a release version`},
		"no chart metadata":      {"metallb", rel("", ""), IdentityUnknown, "the release has no chart metadata"},
		"foreign chart, metallb": {"metallb", rel("bitnami-metallb", "6.4.0"), IdentityLegacy, "chart bitnami-metallb, the catalog manages metallb"},
	} {
		t.Run(name, func(t *testing.T) {
			live := liveCluster()
			live.ClusterState.Releases[tc.key] = tc.release

			chart := ResolveClusterShape(live, nil, shapeManaged).Charts[tc.key]

			require.Equal(t, tc.identity, chart.Identity)
			require.Equal(t, tc.reason, chart.Reason)
		})
	}
}

// Live wins; the record only marks what changed by hand.
func TestResolveClusterShape_RecordedVersusLive(t *testing.T) {
	recorded := liveCluster()
	live := liveCluster()
	live.ClusterState.Releases["metallb"] = rel("metallb", "0.16.0")             // downgraded by hand
	delete(live.ClusterState.Releases, "external-secrets")                       // uninstalled by hand
	live.ClusterState.Releases["alloy"] = rel("alloy", "1.13.1")                 // installed outside the record
	live.ClusterState.Releases["node-exporter"] = rel("node-exporter", "4.59.0") // same version, other chart

	charts := ResolveClusterShape(live, &recorded, shapeManaged).Charts

	require.Equal(t, "0.16.0", charts["metallb"].Live)
	require.Equal(t, "0.16.1", charts["metallb"].Recorded)
	require.True(t, charts["metallb"].Changed)
	require.True(t, charts["external-secrets"].Missing)
	require.True(t, charts["external-secrets"].Changed)
	require.True(t, charts["alloy"].Changed)
	require.True(t, charts["node-exporter"].Changed)
}

// Helm keeps the record of an uninstalled release; the chart is missing, and
// its status is kept for diagnostics.
func TestResolveClusterShape_UninstalledReleaseIsMissing(t *testing.T) {
	recorded := liveCluster()
	live := liveCluster()
	uninstalled := rel("metallb", "0.16.1")
	uninstalled.Status = release.StatusUninstalled
	live.ClusterState.Releases["metallb"] = uninstalled

	metallb := ResolveClusterShape(live, &recorded, shapeManaged).Charts["metallb"]

	require.True(t, metallb.Missing)
	require.Empty(t, metallb.Live)
	require.Empty(t, metallb.Identity)
	require.Equal(t, release.StatusUninstalled, metallb.Status)
	require.True(t, metallb.Changed)
}

// With no state on disk nothing is marked changed.
func TestResolveClusterShape_NoRecord(t *testing.T) {
	charts := ResolveClusterShape(liveCluster(), nil, shapeManaged).Charts

	for key, chart := range charts {
		require.False(t, chart.Changed, key)
		require.Empty(t, chart.Recorded, key)
	}
}

func TestResolveClusterShape_Cilium(t *testing.T) {
	recorded := liveCluster()
	live := liveCluster()
	live.ClusterState.CiliumAgentVersion = "1.19.4" // agent not rolled to the chart's version

	cilium := ResolveClusterShape(live, &recorded, shapeManaged).Cilium

	require.Equal(t, CiliumShape{Chart: "1.20.2", Agent: "1.19.4", Mismatch: true, Recorded: "1.20.2", Changed: true}, cilium)

	live.ClusterState.CiliumAgentVersion = ""
	require.False(t, ResolveClusterShape(live, nil, shapeManaged).Cilium.Mismatch, "an unknown agent is not a mismatch")
}

func TestResolveClusterShape_Kubernetes(t *testing.T) {
	live := liveCluster()
	live.ClusterState.KubeletVersion = "1.33.4" // binary swapped, kubelet not restarted
	live.ClusterState.NodeCount = 2

	k := ResolveClusterShape(live, nil, shapeManaged).Kubernetes

	require.Equal(t, "1.33.4", k.Kubelet)
	require.Equal(t, "1.36.5", k.KubeletBinary)
	require.True(t, k.UnexpectedNodes)
}

func TestResolveClusterShape_Unobserved(t *testing.T) {
	live := liveCluster()
	live.ClusterState.Unobserved = true

	require.False(t, ResolveClusterShape(live, nil, shapeManaged).Observed)
}
