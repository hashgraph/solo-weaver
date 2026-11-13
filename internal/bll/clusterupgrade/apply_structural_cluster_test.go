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

// TestWithCluster_ApplyStructuralHop applies the structural hop for
// HOP_COMPONENT (node-exporter or external-secrets) from HOP_FROM to HOP_TO to
// the live cluster, against the real state directory. Run it as root on a VM:
//
//	HOP_COMPONENT=node-exporter HOP_FROM=4.4.9 HOP_TO=4.59.0 go test -tags require_cluster \
//	  -run '^TestWithCluster_ApplyStructuralHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyStructuralHop(t *testing.T) {
	component, from, to := os.Getenv("HOP_COMPONENT"), os.Getenv("HOP_FROM"), os.Getenv("HOP_TO")
	if component == "" || from == "" || to == "" {
		t.Skip("set HOP_COMPONENT, HOP_FROM and HOP_TO")
	}
	step := StepReplace
	if component == "external-secrets" {
		step = StepMigrate
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

	require.NoError(t, ApplyStructuralHop(context.Background(), src, run,
		Hop{Kind: KindStructural, Step: step, Component: component, From: from, To: to}))
}
