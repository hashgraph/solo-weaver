// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"helm.sh/helm/v3/pkg/release"
)

// Identity says whether a release runs the chart the catalog manages.
type Identity string

const (
	IdentityCurrent Identity = "current"
	// IdentityLegacy is a release of a chart the catalog no longer manages
	// under this name, such as the Bitnami node-exporter or ESO 1.x. It needs
	// a structural migration, not a version upgrade.
	IdentityLegacy Identity = "legacy"
	// IdentityUnknown is a release whose chart could not be read.
	IdentityUnknown Identity = "unknown"
)

// minChartMajor is the lowest chart major the catalog manages, for charts whose
// older majors keep the same chart name.
var minChartMajor = map[string]int{"external-secrets": 2}

// ClusterShape is what the cluster runs, read live, with what state recorded
// beside it. It is the upgrade resolver's input.
type ClusterShape struct {
	// Observed is false when part of the cluster could not be read; the
	// values are then partly the recorded ones.
	Observed   bool
	Kubernetes KubernetesShape
	Cilium     CiliumShape
	// Charts is keyed by catalog name and holds every managed chart, present
	// or not. Cilium is in Cilium, not here.
	Charts map[string]ChartShape
}

type KubernetesShape struct {
	ControlPlane string // the API server's version
	// Kubelet is the version the node reports it runs; KubeletBinary is the
	// binary on the host. They differ between a binary swap and the restart.
	Kubelet          string
	KubeletBinary    string
	Kubeadm          string
	ContainerRuntime string
	NodeCount        int
	// UnexpectedNodes is set when the cluster has more than the one node the
	// provisioner creates.
	UnexpectedNodes bool
}

type CiliumShape struct {
	Chart string
	Agent string
	// Mismatch is set when both versions are known and differ.
	Mismatch bool
	// Recorded is the chart version state recorded; Changed is set when the
	// chart or the agent differs from the record.
	Recorded string
	Changed  bool
}

type ChartShape struct {
	Live      string // chart version, empty when Missing
	ChartName string
	Status    release.Status
	Missing   bool
	Identity  Identity // empty when Missing
	Reason    string   // why the identity is not current
	// Recorded is the chart version state recorded; Changed is set when the
	// release appeared, went away, or runs another chart or version than the
	// record says.
	Recorded string
	Changed  bool
}

// ResolveClusterShape builds the shape from live, the state just read from the
// host and cluster, against recorded, the state on disk before the read (nil
// when there is none). managed lists the releases to report, as
// reality.ManagedReleases returns them. Live values always win; recorded only
// marks what changed.
func ResolveClusterShape(live state.State, recorded *state.State, managed []reality.ManagedRelease) ClusterShape {
	cs := live.ClusterState
	shape := ClusterShape{
		Observed: !cs.Unobserved,
		Kubernetes: KubernetesShape{
			ControlPlane:     strings.TrimPrefix(cs.ServerVersion.GitVersion, "v"),
			Kubelet:          cs.KubeletVersion,
			KubeletBinary:    live.MachineState.Software["kubelet"].Version,
			Kubeadm:          live.MachineState.Software["kubeadm"].Version,
			ContainerRuntime: cs.ContainerRuntimeVersion,
			NodeCount:        cs.NodeCount,
			UnexpectedNodes:  cs.NodeCount > 1,
		},
		Charts: map[string]ChartShape{},
	}

	var rec map[string]state.HelmReleaseInfo
	if recorded != nil {
		rec = recorded.ClusterState.Releases
	}

	for _, m := range managed {
		liveRel, inLive := cs.Releases[m.Key]
		recRel, inRec := rec[m.Key]
		changed := recorded != nil && (inLive != inRec ||
			liveRel.ChartVersion != recRel.ChartVersion || liveRel.ChartName != recRel.ChartName)

		if m.Key == reality.CiliumReleaseKey {
			shape.Cilium = CiliumShape{
				Chart:    liveRel.ChartVersion,
				Agent:    cs.CiliumAgentVersion,
				Mismatch: liveRel.ChartVersion != "" && cs.CiliumAgentVersion != "" && liveRel.ChartVersion != cs.CiliumAgentVersion,
				Recorded: recRel.ChartVersion,
				Changed:  changed || (recorded != nil && cs.CiliumAgentVersion != recorded.ClusterState.CiliumAgentVersion),
			}
			continue
		}

		chart := ChartShape{Recorded: recRel.ChartVersion, Changed: changed, Missing: !inLive}
		if inLive {
			chart.Live = liveRel.ChartVersion
			chart.ChartName = liveRel.ChartName
			chart.Status = liveRel.Status
			chart.Identity, chart.Reason = identityOf(m, liveRel)
		}
		shape.Charts[m.Key] = chart
	}
	return shape
}

// identityOf says whether rel runs the chart m manages.
func identityOf(m reality.ManagedRelease, rel state.HelmReleaseInfo) (Identity, string) {
	if rel.ChartName == "" {
		return IdentityUnknown, "the release has no chart metadata"
	}
	if rel.ChartName != m.Chart {
		return IdentityLegacy, fmt.Sprintf("chart %s, the catalog manages %s", rel.ChartName, m.Chart)
	}
	if min, ok := minChartMajor[m.Key]; ok {
		major, err := chartMajor(rel.ChartVersion)
		if err != nil {
			return IdentityUnknown, fmt.Sprintf("chart version %q is not a release version", rel.ChartVersion)
		}
		if major < min {
			return IdentityLegacy, fmt.Sprintf("chart major %d, the catalog manages %d and later", major, min)
		}
	}
	return IdentityCurrent, ""
}

func chartMajor(version string) (int, error) {
	major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	return strconv.Atoi(major)
}
