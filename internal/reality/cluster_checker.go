// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"maps"
	"path"
	"slices"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/cilium"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	helm2 "github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// CiliumReleaseKey is the ClusterState.Releases key of the Cilium agent, which
// is not a catalog chart.
const CiliumReleaseKey = "cilium"

// clusterChartsRecordedElsewhere are catalog charts whose release is recorded
// by another component, so ClusterState.Releases leaves them out.
var clusterChartsRecordedElsewhere = []string{
	"teleport-cluster-agent", // TeleportState.ClusterAgent
	"solo-operator",
}

// managedRelease identifies one Helm release recorded in ClusterState.Releases.
type managedRelease struct {
	key       string
	release   string
	namespace string
	chart     string // the chart's name, as Helm reports it
	chartRef  string
}

// clusterChecker probes the Kubernetes cluster and returns a ClusterState.
// It depends only on a ClusterProbe and a Helm factory (both injectable) and
// kube.RetrieveClusterInfo.
type clusterChecker struct {
	sm      state.Manager
	newHelm func() (HelmManager, error)
	probe   ClusterProbe
}

// NewClusterChecker constructs a clusterChecker with the given Helm factory
// and probe. NewCheckers passes the production ones; in tests pass fakes.
func NewClusterChecker(sm state.Manager, newHelm func() (HelmManager, error), probe ClusterProbe) (Checker[state.ClusterState], error) {
	return &clusterChecker{sm: sm, newHelm: newHelm, probe: probe}, nil
}

// NewLiveClusterChecker is the cluster checker with the production Helm client
// and cluster probe, for a command that records the cluster without the full
// set of checkers.
func NewLiveClusterChecker(sm state.Manager) Checker[state.ClusterState] {
	return &clusterChecker{sm: sm, newHelm: func() (HelmManager, error) { return helm2.NewManager() }, probe: probeCluster}
}

// RefreshState returns the live cluster. The chart releases are taken from Helm
// only when the cluster answered and Helm listed its releases; otherwise the
// recorded ones are kept. A cluster that could not be fully read is marked
// Unobserved, and a writer then keeps the recorded cluster.
func (c *clusterChecker) RefreshState(_ context.Context) (state.ClusterState, error) {
	cs := state.NewClusterState()
	cs.Releases = maps.Clone(c.sm.State().ClusterState.Releases)

	if !c.probe().Observed() {
		logx.As().Debug().Msg("Kubernetes cluster does not exist or is unreachable, returning empty ClusterState")
		cs.Unobserved = true
		return cs, nil
	}

	releases, err := c.listReleases()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to read the cluster chart releases; keeping the recorded ones")
		cs.Unobserved = true
	} else {
		cs.Releases = releases
	}

	clusterInfo, err := kube.RetrieveClusterInfo()
	if err != nil {
		logx.As().Error().Err(err).Msg("Failed to retrieve cluster info, returning empty ClusterState")
		cs.Unobserved = true
		return cs, nil
	}

	cs.Initialize(clusterInfo)

	logx.As().Debug().Any("clusterState", cs).Msg("Refreshed cluster state")

	return cs, nil
}

// listReleases reads the release of every managed chart from Helm. A chart
// with no release gets no key. A release in any state is recorded with its
// status, so a failed or pending one stays visible to the upgrade resolver.
func (c *clusterChecker) listReleases() (map[string]state.HelmReleaseInfo, error) {
	managed, err := managedReleases()
	if err != nil {
		return nil, err
	}

	hm, err := c.newHelm()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create helm manager")
	}

	all, err := hm.ListAll()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to list helm releases")
	}

	out := map[string]state.HelmReleaseInfo{}
	for _, m := range managed {
		if rel := findRelease(all, m); rel != nil {
			out[m.key] = releaseInfo(rel, m.chartRef)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// managedReleases lists the releases ClusterState.Releases records: every
// catalog chart not recorded elsewhere, then the Cilium agent.
func managedReleases() ([]managedRelease, error) {
	catalog, err := software.LoadInfrastructureCatalog()
	if err != nil {
		return nil, err
	}

	var out []managedRelease
	for _, ch := range catalog.Cluster {
		if slices.Contains(clusterChartsRecordedElsewhere, ch.Name) {
			continue
		}
		out = append(out, managedRelease{key: ch.Name, release: ch.Release, namespace: ch.Namespace, chart: path.Base(ch.Chart), chartRef: ch.Chart})
	}
	return append(out, managedRelease{key: CiliumReleaseKey, release: cilium.ReleaseName, namespace: cilium.Namespace, chart: "cilium"}), nil
}

// findRelease returns m's release in its catalog namespace. Failing that, it
// returns the only release with m's name and chart in any namespace, because
// some charts can be installed elsewhere (eso operator install --namespace).
// It returns nil when there is none, or more than one elsewhere.
func findRelease(releases []*release.Release, m managedRelease) *release.Release {
	var elsewhere []*release.Release
	for _, rel := range releases {
		if rel == nil || rel.Info == nil || rel.Name != m.release {
			continue
		}
		if rel.Namespace == m.namespace {
			return rel
		}
		if rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Name == m.chart {
			elsewhere = append(elsewhere, rel)
		}
	}
	if len(elsewhere) == 1 {
		return elsewhere[0]
	}
	return nil
}

// releaseInfo records rel. Helm does not keep the chart reference, so chartRef
// comes from the catalog; it is empty for Cilium, installed from a local chart.
func releaseInfo(rel *release.Release, chartRef string) state.HelmReleaseInfo {
	info := state.HelmReleaseInfo{
		Name:          rel.Name,
		Namespace:     rel.Namespace,
		ChartRef:      chartRef,
		Status:        rel.Info.Status,
		FirstDeployed: rel.Info.FirstDeployed,
		LastDeployed:  rel.Info.LastDeployed,
		DeletedAt:     rel.Info.Deleted,
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		info.ChartName = rel.Chart.Metadata.Name
		info.ChartVersion = rel.Chart.Metadata.Version
		info.AppVersion = rel.Chart.Metadata.AppVersion
	}
	return info
}
