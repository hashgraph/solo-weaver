//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// TestWithCluster_ApplyKubernetesHop applies the Kubernetes hop KUBE_FROM ->
// KUBE_TO (KUBE_STEP: patch, minor or resume) to the live cluster, against the
// real state directory. Run it as root on a VM:
//
//	KUBE_STEP=patch KUBE_FROM=1.33.4 KUBE_TO=1.33.13 go test -tags require_cluster \
//	  -run '^TestWithCluster_ApplyKubernetesHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyKubernetesHop(t *testing.T) {
	step, from, to := os.Getenv("KUBE_STEP"), os.Getenv("KUBE_FROM"), os.Getenv("KUBE_TO")
	if step == "" || from == "" || to == "" {
		t.Skip("set KUBE_STEP, KUBE_FROM and KUBE_TO")
	}

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
	defer locks.Release()
	client, err := kube.NewClient()
	require.NoError(t, err)
	ctx := kube.WithKubeClient(context.Background(), client)

	require.NoError(t, ApplyKubernetesHop(ctx, src, locks, run,
		Hop{Kind: KindKubernetes, Step: HopStep(step), Component: "kubernetes", From: from, To: to}))
}
