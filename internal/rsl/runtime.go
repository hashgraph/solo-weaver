// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"path/filepath"
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
	BlockNodeRuntime Resolver[state.BlockNodeState, models.BlockNodeInputs]
	ConsensusRuntime Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs]
	ClusterRuntime   Resolver[state.ClusterState, models.ClusterInputs]
	MachineRuntime   Resolver[state.MachineState, models.MachineInputs]
	TeleportRuntime  Resolver[state.TeleportState, models.TeleportNodeInputs]
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

	consensusNodes := currentState.ConsensusNodes
	if consensusNodes == nil {
		consensusNodes = make(map[string]state.ConsensusNodeState)
	}
	consensusRuntime, err := NewConsensusNodeRuntimeResolver(cfg, consensusNodes, realityChecker.Consensus, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise consensus-node runtime")
	}

	machineRuntime, err := NewMachineRuntimeResolver(cfg, currentState.MachineState, realityChecker.Machine, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise machine runtime")
	}

	teleportRuntime, err := NewTeleportRuntimeResolver(cfg, currentState.TeleportState, realityChecker.Teleport, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise teleport runtime")
	}

	return &RuntimeResolver{
		sm:               sm,
		ClusterRuntime:   clusterRuntime,
		BlockNodeRuntime: blockNodeRuntime,
		ConsensusRuntime: consensusRuntime,
		MachineRuntime:   machineRuntime,
		TeleportRuntime:  teleportRuntime,
	}, nil
}

// Refresh forces all runtimes to refresh their state from reality, bypassing any caching or staleness checks.
// This is useful after any user input that may have changed reality, to ensure that subsequent reads reflect the latest reality state.
func (r *RuntimeResolver) Refresh(ctx context.Context, force bool) (state.State, error) {
	if err := r.refreshPersisted(); err != nil {
		return state.State{}, err
	}
	return r.refreshRuntimes(ctx, force)
}

// RefreshWithBaseline refreshes exactly like Refresh and also returns the state
// as persisted on disk before reality was folded in. The baseline is nil when
// there is no state file or it could not be captured; capturing it never fails
// the refresh.
func (r *RuntimeResolver) RefreshWithBaseline(ctx context.Context, force bool) (*state.State, state.State, error) {
	if err := r.refreshPersisted(); err != nil {
		return nil, state.State{}, err
	}

	// Must be taken before the runtimes refresh: they replace the persisted
	// sub-states with reality, and checkers that call sm.Refresh() write
	// through pointers a plain copy would share.
	baseline := r.persistedBaseline()

	current, err := r.refreshRuntimes(ctx, force)
	if err != nil {
		return nil, state.State{}, err
	}
	return baseline, current, nil
}

func (r *RuntimeResolver) persistedBaseline() *state.State {
	_, exists, err := r.sm.HasPersistedState()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to check for a state file; skipping out-of-band change detection")
		return nil
	}
	if !exists {
		return nil
	}

	baseline, err := r.sm.State().PersistedSnapshot()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to snapshot persisted state; skipping out-of-band change detection")
		return nil
	}
	return &baseline
}

func (r *RuntimeResolver) refreshPersisted() error {
	if err := r.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return errorx.IllegalState.Wrap(err, "failed to refresh state")
	}
	return nil
}

func (r *RuntimeResolver) refreshRuntimes(ctx context.Context, force bool) (state.State, error) {
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

	if r.ConsensusRuntime != nil {
		ctx2c, cancel2c := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel2c()
		if err := r.ConsensusRuntime.RefreshState(ctx2c, force); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh consensus node state: %v", err)
		}
	} else {
		logx.As().Debug().Msg("Consensus node runtime is not initialized; skipping refresh")
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

	if r.TeleportRuntime != nil {
		ctx4, cancel4 := context.WithTimeout(ctx, DefaultRefreshTimeout)
		defer cancel4()
		if err := r.TeleportRuntime.RefreshState(ctx4, force); err != nil {
			return state.State{}, errorx.IllegalState.New("failed to refresh teleport state: %v", err)
		}
	} else {
		logx.As().Debug().Msg("Teleport runtime is not initialized; skipping refresh")
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

	consensusNodes, err := r.ConsensusRuntime.CurrentState()
	if err != nil {
		return state.State{}, errorx.IllegalState.New("failed to read consensus node state: %v", err)
	}

	machineState, err := r.MachineRuntime.CurrentState()
	if err != nil {
		return state.State{}, errorx.IllegalState.New("failed to read machine state: %v", err)
	}

	teleportState, err := r.TeleportRuntime.CurrentState()
	if err != nil {
		return state.State{}, errorx.IllegalState.New("failed to read teleport state: %v", err)
	}

	currentState.ClusterState = clusterState
	currentState.BlockNodeState = blockNodeState
	currentState.ConsensusNodes = consensusNodes
	currentState.MachineState = machineState
	currentState.TeleportState = teleportState

	logx.As().Debug().Any("currentState", currentState).Msg("Composed current state from all runtimes")
	return currentState, nil
}

func (r *RuntimeResolver) AddActionHistory(entry state.ActionHistory) state.Writer {
	return r.sm.AddActionHistory(entry)
}

func (r *RuntimeResolver) FlushAll(currentState state.State) error {
	if err := r.sm.Set(currentState).FlushAll(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to flush state to disk")
	}

	return nil
}

// FlushScoped persists only the listed components' state, then flushes the
// pending action history — the scoped equivalent of FlushAll.
func (r *RuntimeResolver) FlushScoped(currentState state.State, ids ...state.ComponentID) error {
	w := r.sm.Set(currentState)
	if err := w.FlushScoped(ids...); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to flush scoped state to disk")
	}
	if err := w.FlushActionHistory(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to flush action history")
	}

	return nil
}

// StateDir returns the directory holding every component's persisted file, for
// callers (BaseHandler) that need to acquire per-component locks before a
// command runs.
func (r *RuntimeResolver) StateDir() string {
	return filepath.Dir(r.sm.State().StateFile)
}
