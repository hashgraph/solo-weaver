// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	k8sversion "k8s.io/apimachinery/pkg/version"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

type fakeChecker[T any] struct {
	value T
	err   error
}

func (f fakeChecker[T]) RefreshState(context.Context) (T, error) { return f.value, f.err }

func probeReturns(r reality.ClusterReachability) reality.ClusterProbe {
	return func() reality.ClusterReachability { return r }
}

func deployed(chart, version string) state.HelmReleaseInfo {
	return state.HelmReleaseInfo{ChartName: chart, ChartVersion: version, Status: release.StatusDeployed}
}

// v035Cluster is a cluster as v0.35.0 left it: Kubernetes 1.33.4 with
// Cilium 1.18.1, MetalLB 0.15.2 and metrics-server 3.13.0.
func v035Cluster() state.ClusterState {
	var cs state.ClusterState
	cs.ServerVersion = k8sversion.Info{GitVersion: "v1.33.4"}
	cs.KubeletVersion = "1.33.4"
	cs.NodeCount = 1
	cs.CiliumAgentVersion = "1.18.1"
	cs.Releases = map[string]state.HelmReleaseInfo{
		"cilium":         deployed("cilium", "1.18.1"),
		"metallb":        deployed("metallb", "0.15.2"),
		"metrics-server": deployed("metrics-server", "3.13.0"),
	}
	return cs
}

func liveSources(t *testing.T, cluster state.ClusterState) (LiveSources, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), state.StateFileName)
	sm, err := state.NewStateManager(state.WithStateFile(file))
	require.NoError(t, err)
	return LiveSources{
		State:   sm,
		Cluster: fakeChecker[state.ClusterState]{value: cluster},
		Machine: fakeChecker[state.MachineState]{value: state.MachineState{Software: map[string]state.SoftwareState{}}},
		Managed: reality.ManagedReleases,
		Catalog: software.LoadInfrastructureCatalog,
		Probe:   probeReturns(reality.Reachable),
	}, filepath.Dir(file)
}

func TestPlanLive_PlansTheV035Cluster(t *testing.T) {
	src, dir := liveSources(t, v035Cluster())

	plan, err := PlanLive(context.Background(), src, Options{})
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	assert.Equal(t, Target{Kubernetes: "1.36.5", Cilium: "1.20.2"}, plan.Target)
	assert.Equal(t, []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		ciliumHop(StepMinor, "1.18.16", "1.19.10"),
		kubeHop(StepPatch, "1.33.4", "1.33.13"),
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
		ciliumHop(StepMinor, "1.19.10", "1.20.2"),
		kubeHop(StepMinor, "1.34.13", "1.35.10"),
		coupledHop("1.36", bump("metallb", "0.15.2", "0.16.1")),
		kubeHop(StepMinor, "1.35.10", "1.36.5"),
		chartsHop(StepRefresh, bump("metrics-server", "3.13.0", "3.14.0")),
	}, plan.Hops)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "planning must write nothing")
}

// After the first hop lands, the next run plans one step fewer.
func TestPlanLive_RecomputesAfterAHop(t *testing.T) {
	cs := v035Cluster()
	cs.CiliumAgentVersion = "1.18.16"
	cs.Releases["cilium"] = deployed("cilium", "1.18.16")
	src, _ := liveSources(t, cs)

	plan, err := PlanLive(context.Background(), src, Options{})
	require.NoError(t, err)
	assert.Len(t, plan.Hops, 8)
	assert.Equal(t, ciliumHop(StepMinor, "1.18.16", "1.19.10"), plan.Hops[0])
}

// The shape carries the record, so a release that went away since it was
// recorded is seen.
func TestReadLive_CarriesTheRecord(t *testing.T) {
	recorded := v035Cluster()
	recorded.Releases["node-exporter"] = deployed("node-exporter", "4.4.9")
	src, _ := liveSources(t, v035Cluster())
	st := src.State.State()
	st.ClusterState = recorded
	_, err := src.State.Set(st).Persist(state.ComponentCluster)
	require.NoError(t, err)

	shape, _, _, err := readLive(context.Background(), src)

	require.NoError(t, err)
	ne := shape.Charts["node-exporter"]
	assert.True(t, ne.Missing)
	assert.Equal(t, "4.4.9", ne.Recorded)
	assert.Equal(t, rsl.IdentityLegacy, ne.RecordedIdentity)
}

func TestPlanLive_UnreadClusterBlocks(t *testing.T) {
	cs := v035Cluster()
	cs.Unobserved = true
	src, _ := liveSources(t, cs)

	plan, err := PlanLive(context.Background(), src, Options{})
	require.NoError(t, err)
	assert.Empty(t, plan.Hops)
	require.NotEmpty(t, plan.Blockers)
	assert.Equal(t, BlockerNotObserved, plan.Blockers[0].Code)
}

// An unreachable cluster blocks before the cluster is read: the checker would
// return the record, so a cluster fake that fails proves it was not called.
func TestPlanLive_UnreachableClusterBlocksWithTheCause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		probe  reality.ClusterReachability
		detail string
	}{
		{"no kubeconfig", reality.NotConfigured, "run the command with sudo, or set KUBECONFIG"},
		{"API server silent", reality.Unobservable, "kubectl get --raw /readyz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, _ := liveSources(t, v035Cluster())
			src.Probe = probeReturns(tc.probe)
			src.Cluster = fakeChecker[state.ClusterState]{err: errorx.IllegalState.New("the cluster must not be read")}

			plan, err := PlanLive(context.Background(), src, Options{})
			require.NoError(t, err)
			assert.Empty(t, plan.Hops)
			assert.Equal(t, Target{Kubernetes: "1.36.5"}, plan.Target)
			require.Len(t, plan.Blockers, 1)
			assert.Equal(t, BlockerNotObserved, plan.Blockers[0].Code)
			assert.Contains(t, plan.Blockers[0].Detail, tc.detail)
		})
	}
}

func TestPlanLive_UnreachableClusterStillChecksTheTarget(t *testing.T) {
	src, _ := liveSources(t, v035Cluster())
	src.Probe = probeReturns(reality.NotConfigured)
	_, err := PlanLive(context.Background(), src, Options{To: "1.37"})
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
}

func TestPlanLive_Errors(t *testing.T) {
	boom := errorx.ExternalError.New("boom")
	tests := []struct {
		name    string
		mutate  func(*LiveSources)
		wantErr string
	}{
		{"missing source", func(s *LiveSources) { s.Managed = nil }, "every live source must be set"},
		{"missing probe", func(s *LiveSources) { s.Probe = nil }, "every live source must be set"},
		{"cluster read fails", func(s *LiveSources) { s.Cluster = fakeChecker[state.ClusterState]{err: boom} }, "failed to read the cluster"},
		{"host read fails", func(s *LiveSources) { s.Machine = fakeChecker[state.MachineState]{err: boom} }, "failed to read the host"},
		{"managed list fails", func(s *LiveSources) {
			s.Managed = func() ([]reality.ManagedRelease, error) { return nil, boom }
		}, "failed to list the managed releases"},
		{"catalog fails", func(s *LiveSources) {
			s.Catalog = func() (*software.InfrastructureCatalog, error) { return nil, boom }
		}, "failed to load the catalog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, _ := liveSources(t, v035Cluster())
			tt.mutate(&src)
			_, err := PlanLive(context.Background(), src, Options{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPlanLive_BadTargetIsAnArgumentError(t *testing.T) {
	src, _ := liveSources(t, v035Cluster())
	_, err := PlanLive(context.Background(), src, Options{To: "1.37"})
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
}
