// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/cilium"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	helm2 "github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

// ManagedRelease identifies one Helm release recorded in ClusterState.Releases.
type ManagedRelease struct {
	Key       string
	Release   string
	Namespace string
	Chart     string // the chart's name, as Helm reports it
	ChartRef  string // empty for Cilium, installed from a local chart
}

// NodeInfo is what a node reports it runs.
type NodeInfo struct {
	Name                    string
	KubeletVersion          string
	ContainerRuntimeVersion string
}

// ClusterReader reads the live cluster facts recorded beside the Helm releases.
type ClusterReader interface {
	Nodes(ctx context.Context) ([]NodeInfo, error)
	// CiliumAgentVersion returns false when the agent DaemonSet does not exist.
	CiliumAgentVersion(ctx context.Context) (string, bool, error)
}

// clusterChecker probes the Kubernetes cluster and returns a ClusterState.
// It depends only on a ClusterProbe, a Helm factory and a ClusterReader
// factory (all injectable) and kube.RetrieveClusterInfo.
type clusterChecker struct {
	sm        state.Manager
	newHelm   func() (HelmManager, error)
	newReader func() (ClusterReader, error)
	probe     ClusterProbe
}

// NewClusterChecker constructs a clusterChecker. NewCheckers passes the
// production factories and probe; in tests pass fakes.
func NewClusterChecker(
	sm state.Manager,
	newHelm func() (HelmManager, error),
	newReader func() (ClusterReader, error),
	probe ClusterProbe,
) (Checker[state.ClusterState], error) {
	return &clusterChecker{sm: sm, newHelm: newHelm, newReader: newReader, probe: probe}, nil
}

// NewLiveClusterChecker is the cluster checker with the production clients and
// cluster probe, for a command that records the cluster without the full set
// of checkers.
func NewLiveClusterChecker(sm state.Manager) Checker[state.ClusterState] {
	return &clusterChecker{
		sm:        sm,
		newHelm:   func() (HelmManager, error) { return helm2.NewManager() },
		newReader: newKubeClusterReader,
		probe:     probeCluster,
	}
}

// RefreshState returns the live cluster. Each live fact (chart releases, node,
// Cilium agent) is taken from the cluster only when it could be read;
// otherwise the recorded value is kept and the result is marked Unobserved,
// and a writer then keeps the recorded cluster.
func (c *clusterChecker) RefreshState(ctx context.Context) (state.ClusterState, error) {
	recorded := c.sm.State().ClusterState
	cs := state.NewClusterState()
	cs.Releases = maps.Clone(recorded.Releases)
	cs.KubeletVersion = recorded.KubeletVersion
	cs.ContainerRuntimeVersion = recorded.ContainerRuntimeVersion
	cs.NodeCount = recorded.NodeCount
	cs.CiliumAgentVersion = recorded.CiliumAgentVersion

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

	if err := c.readCluster(ctx, &cs); err != nil {
		logx.As().Warn().Err(err).Msg("Failed to read the node or the Cilium agent; keeping the recorded values")
		cs.Unobserved = true
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

// readCluster sets the node and Cilium agent facts of cs. On an error it
// leaves each fact it could not read as it was.
func (c *clusterChecker) readCluster(ctx context.Context, cs *state.ClusterState) error {
	r, err := c.newReader()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create kube client")
	}

	nodes, nodesErr := r.Nodes(ctx)
	if nodesErr == nil {
		node := hostNode(nodes)
		cs.KubeletVersion = strings.TrimPrefix(node.KubeletVersion, "v")
		cs.ContainerRuntimeVersion = node.ContainerRuntimeVersion
		cs.NodeCount = len(nodes)
	}

	agent, found, agentErr := r.CiliumAgentVersion(ctx)
	if agentErr == nil {
		cs.CiliumAgentVersion = ""
		if found {
			cs.CiliumAgentVersion = agent
		}
	}

	if nodesErr != nil {
		return nodesErr
	}
	return agentErr
}

// hostNode returns the node named after this host, or the first node: the
// provisioner creates single-node clusters, so the choice only matters when
// someone joined more nodes by hand.
func hostNode(nodes []NodeInfo) NodeInfo {
	if len(nodes) == 0 {
		return NodeInfo{}
	}
	if host, err := os.Hostname(); err == nil {
		for _, n := range nodes {
			if n.Name == host {
				return n
			}
		}
	}
	return nodes[0]
}

// listReleases reads the release of every managed chart from Helm. A chart
// with no release gets no key. A release in any state is recorded with its
// status, so a failed or pending one stays visible to the upgrade resolver.
func (c *clusterChecker) listReleases() (map[string]state.HelmReleaseInfo, error) {
	managed, err := ManagedReleases()
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
			out[m.Key] = releaseInfo(rel, m)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ManagedReleases lists the releases ClusterState.Releases records: every
// catalog chart not recorded elsewhere, then the Cilium agent.
func ManagedReleases() ([]ManagedRelease, error) {
	catalog, err := software.LoadInfrastructureCatalog()
	if err != nil {
		return nil, err
	}

	var out []ManagedRelease
	for _, ch := range catalog.Cluster {
		if slices.Contains(clusterChartsRecordedElsewhere, ch.Name) {
			continue
		}
		out = append(out, ManagedRelease{Key: ch.Name, Release: ch.Release, Namespace: ch.Namespace, Chart: path.Base(ch.Chart), ChartRef: ch.Chart})
	}
	return append(out, ManagedRelease{Key: CiliumReleaseKey, Release: cilium.ReleaseName, Namespace: cilium.Namespace, Chart: "cilium"}), nil
}

// findRelease returns m's release in its catalog namespace. Failing that, it
// returns the only release with m's name and chart in any namespace, because
// some charts can be installed elsewhere (eso operator install --namespace).
// It returns nil when there is none, or more than one elsewhere.
func findRelease(releases []*release.Release, m ManagedRelease) *release.Release {
	var elsewhere []*release.Release
	for _, rel := range releases {
		if rel == nil || rel.Info == nil || rel.Name != m.Release {
			continue
		}
		if rel.Namespace == m.Namespace {
			return rel
		}
		if rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Name == m.Chart {
			elsewhere = append(elsewhere, rel)
		}
	}
	if len(elsewhere) == 1 {
		return elsewhere[0]
	}
	return nil
}

// releaseInfo records rel. Helm does not keep the chart reference, so it comes
// from the catalog, and only when rel runs the catalog's chart: a release of
// another chart under the same name (the Bitnami node-exporter) must not look
// like the catalog's.
func releaseInfo(rel *release.Release, m ManagedRelease) state.HelmReleaseInfo {
	info := state.HelmReleaseInfo{
		Name:          rel.Name,
		Namespace:     rel.Namespace,
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
	if info.ChartName == m.Chart {
		info.ChartRef = m.ChartRef
	}
	return info
}

// kubeClusterReader is the production ClusterReader.
type kubeClusterReader struct{ kc *kube.Client }

func newKubeClusterReader() (ClusterReader, error) {
	kc, err := kube.NewClient()
	if err != nil {
		return nil, err
	}
	return kubeClusterReader{kc: kc}, nil
}

func (r kubeClusterReader) Nodes(ctx context.Context) ([]NodeInfo, error) {
	list, err := r.kc.List(ctx, kube.KindNode, "", kube.WaitOptions{})
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to list nodes")
	}
	nodes := make([]NodeInfo, 0, len(list.Items))
	for _, item := range list.Items {
		kubelet, _, _ := unstructured.NestedString(item.Object, "status", "nodeInfo", "kubeletVersion")
		runtime, _, _ := unstructured.NestedString(item.Object, "status", "nodeInfo", "containerRuntimeVersion")
		nodes = append(nodes, NodeInfo{Name: item.GetName(), KubeletVersion: kubelet, ContainerRuntimeVersion: runtime})
	}
	return nodes, nil
}

func (r kubeClusterReader) CiliumAgentVersion(ctx context.Context) (string, bool, error) {
	return cilium.AgentVersion(ctx, r.kc.Dyn)
}
