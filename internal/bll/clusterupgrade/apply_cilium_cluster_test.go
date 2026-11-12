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

// TestWithCluster_ApplyCiliumHop applies the Cilium hop CILIUM_FROM ->
// CILIUM_TO to the live cluster, against the real state directory. Run it as
// root on a VM:
//
//	CILIUM_FROM=1.18.1 CILIUM_TO=1.18.16 go test -tags require_cluster \
//	  -run '^TestWithCluster_ApplyCiliumHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyCiliumHop(t *testing.T) {
	from, to := os.Getenv("CILIUM_FROM"), os.Getenv("CILIUM_TO")
	if from == "" || to == "" {
		t.Skip("set CILIUM_FROM and CILIUM_TO")
	}
	f, err := parseVersion(from)
	require.NoError(t, err)
	tv, err := parseVersion(to)
	require.NoError(t, err)
	step := StepMinor
	if f.minorOf() == tv.minorOf() {
		step = StepPatch
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

	require.NoError(t, ApplyCiliumHop(context.Background(), src, run,
		Hop{Kind: KindCilium, Step: step, Component: "cilium", From: from, To: to}))
}
