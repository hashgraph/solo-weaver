//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// liveSourcesOnHost reads the real state directory and cluster. It holds the
// system and cluster locks for the test and puts the kube client in the
// returned context.
func liveSourcesOnHost(t *testing.T) (context.Context, LiveSources, *state.HeldLocks, WorkflowRunner) {
	t.Helper()
	sm, err := state.NewStateManager()
	require.NoError(t, err)
	checkers, err := reality.NewCheckers(sm)
	require.NoError(t, err)
	src := LiveSources{
		State:   sm,
		Cluster: checkers.Cluster,
		Machine: checkers.Machine,
		Managed: reality.ManagedReleases,
		Catalog: software.LoadInfrastructureCatalog,
		Probe:   checkers.Probe,
	}
	run := func(_ context.Context, fn func() (*automa.Report, error)) error {
		report, err := fn()
		if err != nil {
			return err
		}
		return steps.FailureOf(report)
	}
	locks, err := state.AcquireHeldLocks(filepath.Dir(sm.State().StateFile),
		[]state.ComponentID{state.ComponentSystem, state.ComponentCluster}, 0)
	require.NoError(t, err)
	t.Cleanup(locks.Release)
	client, err := kube.NewClient()
	require.NoError(t, err)
	return kube.WithKubeClient(context.Background(), client), src, locks, run
}

// applyHop runs hop with the executor for its kind.
func applyHop(ctx context.Context, src LiveSources, locks *state.HeldLocks, run WorkflowRunner, hop Hop) error {
	switch hop.Kind {
	case KindCilium:
		return ApplyCiliumHop(ctx, src, locks, run, hop)
	case KindKubernetes:
		return ApplyKubernetesHop(ctx, src, locks, run, hop)
	case KindStructural:
		return ApplyStructuralHop(ctx, src, locks, run, hop)
	case KindCharts:
		return ApplyChartsHop(ctx, src, locks, run, hop)
	}
	return errorx.IllegalArgument.New("no executor for %s hops", hop.Kind)
}

// TestWithCluster_ApplyNextHop applies the next hop of the live cluster's plan,
// whatever its kind, and logs it. With HOP_CHARTS ("chart=from->to,...") and
// HOP_STEP (coupled or refresh, plus HOP_KUBERNETES for coupled) it applies
// that charts hop instead. Run it as root on a VM:
//
//	go test -tags require_cluster -run '^TestWithCluster_ApplyNextHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyNextHop(t *testing.T) {
	ctx, src, locks, run := liveSourcesOnHost(t)

	var hop Hop
	if charts := os.Getenv("HOP_CHARTS"); charts != "" {
		hop = Hop{Kind: KindCharts, Step: HopStep(os.Getenv("HOP_STEP")), Kubernetes: os.Getenv("HOP_KUBERNETES")}
		for _, c := range strings.Split(charts, ",") {
			name, move, _ := strings.Cut(c, "=")
			from, to, _ := strings.Cut(move, "->")
			hop.Charts = append(hop.Charts, ChartBump{Chart: name, From: from, To: to})
		}
	} else {
		plan, err := PlanLive(ctx, src, Options{})
		require.NoError(t, err)
		require.Empty(t, plan.Blockers)
		if len(plan.Hops) == 0 {
			t.Log("hop: none, the cluster is at its target")
			return
		}
		hop = plan.Hops[0]
	}
	t.Logf("hop: %+v", hop)
	require.NoError(t, applyHop(ctx, src, locks, run, hop))
}
