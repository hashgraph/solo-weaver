// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Runtime holds the fully-initialised runtime components for all node types.
// It is constructed once at the composition root (main / command init) and injected
// into every layer that needs it, eliminating package-level singletons.
type Runtime struct {
	sm               state.Manager
	BlockNodeRuntime *BlockNodeRuntimeState
	ClusterRuntime   *ClusterRuntimeState
	MachineRuntime   *MachineRuntimeState
}

// NewRuntime creates a Runtime by constructing each runtime from the supplied
// configuration, current persisted state, reality checker, and refresh interval.
func NewRuntime(
	cfg models.Config,
	sm state.Manager,
	realityChecker reality.Checker,
	refreshInterval time.Duration,
) (*Runtime, error) {
	if realityChecker == nil {
		return nil, errorx.IllegalArgument.New("reality checker cannot be nil")
	}

	currentState := sm.State()
	clusterRuntime, err := NewClusterRuntime(cfg, currentState.ClusterState, realityChecker, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise cluster runtime")
	}

	blockNodeRuntime, err := NewBlockNodeRuntime(cfg, currentState.BlockNodeState, realityChecker, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise block-node runtime")
	}

	machineRuntime, err := NewMachineRuntime(cfg, currentState.MachineState, realityChecker, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise machine runtime")
	}

	return &Runtime{
		sm:               sm,
		ClusterRuntime:   clusterRuntime,
		BlockNodeRuntime: blockNodeRuntime,
		MachineRuntime:   machineRuntime,
	}, nil
}

// SetUserInputs accepts inputs as an untyped value and asserts the concrete
// parameterized type required by each runtime before forwarding the call.
func (r *Runtime) SetUserInputs(target models.TargetType, inputs any) error {
	switch target {
	case models.TargetBlockNode:
		if r.BlockNodeRuntime == nil {
			return errorx.IllegalState.New("block node runtime is not initialized")
		}
		in, ok := inputs.(models.BlocknodeInputs)
		if !ok {
			return errorx.IllegalArgument.New("invalid inputs type for target blocknode: %T", inputs)
		}
		return r.BlockNodeRuntime.SetUserInputs(in)
	default:
		return nil
	}
}

// Refresh forces all runtimes to refresh their state from reality, bypassing any caching or staleness checks.
// This is useful after any user input that may have changed reality, to ensure that subsequent reads reflect the latest reality state.
func (r *Runtime) Refresh(ctx context.Context) (state.State, error) {
	if err := r.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return state.State{}, errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	if r.ClusterRuntime != nil {
		ctx1, cancel1 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel1()
		if err := r.ClusterRuntime.RefreshState(ctx1, false); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh cluster state: %v", err)
		}
	}

	if r.BlockNodeRuntime != nil {
		ctx2, cancel2 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel2()
		if err := r.BlockNodeRuntime.RefreshState(ctx2, false); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh block node state: %v", err)
		}
	}

	if r.MachineRuntime != nil {
		ctx3, cancel3 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel3()
		if err := r.MachineRuntime.RefreshState(ctx3, false); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh machine state: %v", err)
		}
	}

	return r.CurrentState()
}

// CurrentState reads the current state from all runtimes and composes it into a single state.State struct.
// This is the single source of truth for all state reads in the CLI layer, ensuring that all reads are consistent and
// reflect the latest reality state (subject to each runtime's staleness checks).
// Ensure to call Refresh() before this to ensure that all runtimes have the latest reality state
func (r *Runtime) CurrentState() (state.State, error) {
	currentState := r.sm.State()
	clusterState, err := r.ClusterRuntime.CurrentState()
	if err != nil {
		return state.State{}, errorx.IllegalState.New("failed to read cluster state: %v", err)
	}

	blockNodeState, err := r.BlockNodeRuntime.CurrentState()
	if err != nil {
		return state.State{}, errorx.IllegalState.New("failed to read block node state: %v", err)
	}

	machineState, err := r.MachineRuntime.CurrentState()
	if err != nil {
		return state.State{}, errorx.IllegalState.New("failed to read machine state: %v", err)
	}

	currentState.ClusterState = clusterState
	currentState.BlockNodeState = blockNodeState
	currentState.MachineState = machineState

	return currentState, nil
}

func (r *Runtime) AddActionHistory(entry state.ActionHistory) state.Writer {
	return r.sm.AddActionHistory(entry)
}

func (r *Runtime) Flush(currentState state.State) error {
	if err := r.sm.Set(currentState).Flush(); err != nil {
		return errorx.IllegalState.New("failed to flush state to disk: %v", err)
	}

	return nil
}
