//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"

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
	}
	run := func(ctx context.Context, wb *automa.WorkflowBuilder) error {
		wf, err := wb.Build()
		if err != nil {
			return err
		}
		return steps.FailureOf(wf.Execute(ctx))
	}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, run,
		Hop{Kind: KindKubernetes, Step: HopStep(step), Component: "kubernetes", From: from, To: to}))
}
