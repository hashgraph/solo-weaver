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

// TestWithCluster_ApplyNextHop runs the upgrade engine on the live cluster:
// the next hop of its plan, or with HOP_ALL=1 every remaining hop, as
// 'kube cluster upgrade' does. With HOP_CHARTS ("chart=from->to,...") and
// HOP_STEP (coupled or refresh, plus HOP_KUBERNETES for coupled) it applies
// that charts hop instead. Run it as root on a VM:
//
//	go test -tags require_cluster -run '^TestWithCluster_ApplyNextHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyNextHop(t *testing.T) {
	ctx, src, locks, run := liveSourcesOnHost(t)

	if charts := os.Getenv("HOP_CHARTS"); charts != "" {
		hop := Hop{Kind: KindCharts, Step: HopStep(os.Getenv("HOP_STEP")), Kubernetes: os.Getenv("HOP_KUBERNETES")}
		for _, c := range strings.Split(charts, ",") {
			name, move, _ := strings.Cut(c, "=")
			from, to, _ := strings.Cut(move, "->")
			hop.Charts = append(hop.Charts, ChartBump{Chart: name, From: from, To: to})
		}
		t.Logf("hop: %s", hop.Describe())
		require.NoError(t, ApplyHop(ctx, src, locks, run, hop))
		return
	}

	res, err := Apply(ctx, src, locks, run, ApplyOptions{All: os.Getenv("HOP_ALL") == "1"})
	for _, h := range res.Applied {
		t.Logf("applied: %s", h.Describe())
	}
	t.Logf("outcome: %s, remaining: %d %s", res.Outcome, len(res.Remaining), res.Cause)
	require.NoError(t, err)
}
