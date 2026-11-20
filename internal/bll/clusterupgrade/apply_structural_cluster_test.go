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

	require.NoError(t, ApplyStructuralHop(ctx, src, locks, run,
		Hop{Kind: KindStructural, Step: step, Component: component, From: from, To: to}))
}
