// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// handleBlockNodeIntentWithCallback is handleBlockNodeIntentWith plus a flush
// callback. HandleIntent invokes the callback AFTER its pre-flush
// Runtime.Refresh (which captures this handler's machine.yaml baseline) and
// BEFORE FlushScoped writes machine.yaml — the exact seam where a second
// process can slip a machine.yaml write in underneath this one.
func handleBlockNodeIntentWithCallback(
	t *testing.T,
	stateFile string,
	h *host,
	intent noopIntent,
	callback func(full *state.State, effInputs models.UserInputs[struct{}]) error,
	managed ...Component,
) (*automa.Report, error) {
	t.Helper()
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())

	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) {
			return state.ClusterState{Created: true}, nil
		}),
		Machine: funcChecker[state.MachineState](func() (state.MachineState, error) {
			return sm.State().MachineState, nil
		}),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) {
			return sm.State().BlockNodeState, nil
		}),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) {
			return h.teleport, nil
		}),
	}
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	base, err := NewBaseHandler[struct{}](runtime, models.TargetBlockNode, WithManagedComponents[struct{}](managed...))
	require.NoError(t, err)

	inputs := models.UserInputs[struct{}]{Common: models.CommonInputs{
		ExecutionOptions: models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.StopOnError},
	}}
	return base.HandleIntent(context.Background(),
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}, inputs, intent, callback)
}

// TestHandleIntent_DisjointHandlersCollideOnMachineFile demonstrates that two
// commands managing *disjoint* components still contend on the unlocked,
// written-by-everyone machine.yaml — the gap TestHandleIntent_DisjointHandlers
// DoNotBlockEachOther cannot see, because that test blocks inside the workflow
// (before the pre-flush Refresh) so the slow handler re-reads the other's
// machine write as its own baseline and commits cleanly.
//
// Here handler A (BlockNode-only) blocks in its flush callback — AFTER
// HandleIntent's pre-flush Refresh captured A's machine.yaml baseline, BEFORE
// A's FlushScoped writes machine.yaml. While A is parked there, handler B
// (Teleport-only) runs to completion and writes machine.yaml. When A resumes,
// its FlushScoped finds machine.yaml changed from the baseline it captured and
// aborts.
//
// Two separate state managers (one per handler) model two separate processes
// (CLI vs daemon, or two CLI invocations) with independent baselines — the
// only configuration where the flock is the sole cross-writer guard, and
// machine.yaml has no flock.
//
// EXPECTED ONCE FIXED: both A and B succeed (disjoint commands never contend).
// ON CURRENT CODE THIS TEST FAILS: A returns
//
//	failed to persist state after workflow: ... machine state file changed
//	externally on disk ... aborting flush to avoid overwrite
//
// Land it together with the machine-contention fix (or t.Skip it referencing
// the tracking issue until then) so the suite stays green.
//
// Tracking issue: hashgraph/solo-weaver#1242.
func TestHandleIntent_DisjointHandlersCollideOnMachineFile(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	aRefreshed := make(chan struct{}) // A has captured its machine.yaml baseline
	bDone := make(chan struct{})      // B has finished writing machine.yaml

	var aErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, aErr = handleBlockNodeIntentWithCallback(t, stateFile, &host{}, noopIntent{},
			func(_ *state.State, _ models.UserInputs[struct{}]) error {
				close(aRefreshed)
				<-bDone
				return nil
			},
			BlockNode)
	}()

	// Wait until A is parked between its pre-flush Refresh and its flush.
	select {
	case <-aRefreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler A never reached its flush callback")
	}

	// B, a Teleport-only command, runs to completion and writes machine.yaml.
	_, bErr := handleBlockNodeIntentWith(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{}, Teleport)
	require.NoError(t, bErr, "B (teleport-only) should complete independently")

	close(bDone) // release A to flush
	wg.Wait()

	// The intended guarantee: a BlockNode-only command must not fail because a
	// Teleport-only command touched machine.yaml. This assertion fails today.
	require.NoError(t, aErr,
		"A (blocknode-only) must not fail just because B (teleport-only) wrote the shared, unlocked machine.yaml")

	// Confirm both components' own files reflect their writes.
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
}
