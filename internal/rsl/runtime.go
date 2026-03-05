// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// RuntimeResolver holds the fully-initialised runtime components for all node types.
// It is constructed once at the composition root (main / command init) and injected
// into every layer that needs it, eliminating package-level singletons.
type RuntimeResolver struct {
	sm               state.Manager
	BlockNodeRuntime *BlockNodeRuntimeResolver
	ClusterRuntime   *ClusterRuntimeResolver
	MachineRuntime   *MachineRuntimeResolver
}

// NewRuntimeResolver creates a RuntimeResolver by constructing each runtime from the supplied
// configuration, current persisted state, reality checker, and refresh interval.
func NewRuntimeResolver(
	cfg models.Config,
	sm state.Manager,
	realityChecker reality.Checkers,
	refreshInterval time.Duration,
) (*RuntimeResolver, error) {
	currentState := sm.State()
	clusterRuntime, err := NewClusterRuntimeResolver(cfg, currentState.ClusterState, realityChecker.Cluster, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise cluster runtime")
	}

	blockNodeRuntime, err := NewBlockNodeRuntimeResolver(cfg, currentState.BlockNodeState, realityChecker.BlockNode, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise block-node runtime")
	}

	machineRuntime, err := NewMachineRuntimeResolver(cfg, currentState.MachineState, realityChecker.Machine, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise machine runtime")
	}

	return &RuntimeResolver{
		sm:               sm,
		ClusterRuntime:   clusterRuntime,
		BlockNodeRuntime: blockNodeRuntime,
		MachineRuntime:   machineRuntime,
	}, nil
}

// Refresh forces all runtimes to refresh their state from reality, bypassing any caching or staleness checks.
// This is useful after any user input that may have changed reality, to ensure that subsequent reads reflect the latest reality state.
func (r *RuntimeResolver) Refresh(ctx context.Context, force bool) (state.State, error) {
	if err := r.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return state.State{}, errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	if r.ClusterRuntime != nil {
		ctx1, cancel1 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel1()
		if err := r.ClusterRuntime.RefreshState(ctx1, force); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh cluster state: %v", err)
		}
	} else {
		logx.As().Debug().Msg("Cluster runtime is not initialized; skipping refresh")
	}

	if r.BlockNodeRuntime != nil {
		ctx2, cancel2 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel2()
		if err := r.BlockNodeRuntime.RefreshState(ctx2, force); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh block node state: %v", err)
		}
	} else {
		logx.As().Debug().Msg("Block node runtime is not initialized; skipping refresh")
	}

	if r.MachineRuntime != nil {
		ctx3, cancel3 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel3()
		if err := r.MachineRuntime.RefreshState(ctx3, force); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh machine state: %v", err)
		}
	} else {
		logx.As().Debug().Msg("Machine runtime is not initialized; skipping refresh")
	}

	return r.CurrentState()
}

// CurrentState reads the current state from all runtimes and composes it into a single state.State struct.
// This is the single source of truth for all state reads in the CLI layer, ensuring that all reads are consistent and
// reflect the latest reality state (subject to each runtime's staleness checks).
// Ensure to call Refresh() before this to ensure that all runtimes have the latest reality state
func (r *RuntimeResolver) CurrentState() (state.State, error) {
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

	logx.As().Debug().Any("currentState", currentState).Msg("Composed current state from all runtimes")
	return currentState, nil
}

func (r *RuntimeResolver) AddActionHistory(entry state.ActionHistory) state.Writer {
	return r.sm.AddActionHistory(entry)
}

func (r *RuntimeResolver) FlushAll(currentState state.State) error {
	if err := r.sm.Set(currentState).FlushAll(); err != nil {
		return errorx.IllegalState.New("failed to flush state to disk: %v", err)
	}

	return nil
}
