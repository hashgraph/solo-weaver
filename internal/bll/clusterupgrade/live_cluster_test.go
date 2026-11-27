//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
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
