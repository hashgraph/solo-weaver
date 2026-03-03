// SPDX-License-Identifier: Apache-2.0

package bll

// Package bll (business logic layer) owns intent routing, effective-value
// resolution, and workflow orchestration.  It sits between the CLI commands
// (cmd layer) and the workflow / step layer.
//
// Shared infrastructure lives in BaseHandler so that every per-node-type
// handler (BlockNode, MirrorNode, RelayNode, …) inherits it without duplication.

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// ── BaseHandler ───────────────────────────────────────────────────────────

// BaseHandler holds the three dependencies that every node-type handler
// needs: a StateManager for reading and writing state, the rsl Registry, and
// the reality Checker for live machine-state queries.
//
// Embed this struct in any concrete handler to inherit RefreshRuntimeState and
// FlushNodeState without re-implementing them.
//
//	type BlockNodeHandler struct {
//	    BaseHandler
//	    install   *BlockNodeInstallHandler
//	    upgrade   *BlockNodeUpgradeHandler
//	    reset     *BlockNodeResetHandler
//	    uninstall *BlockNodeUninstallHandler
//	}
type BaseHandler struct {
	StateManager state.Manager
	RSL          *rsl.Registry
	Checker      reality.Checker // Reality checker is required for live refreshes during FlushNodeState
}

// NewBaseHandler validates the required dependencies and returns a
// populated BaseHandler.  All fields are required; any nil returns an error.
func NewBaseHandler(sm state.Manager, reg *rsl.Registry, checker reality.Checker) (BaseHandler, error) {
	if sm == nil {
		return BaseHandler{}, errorx.IllegalArgument.New("state.Manager cannot be nil")
	}
	if reg == nil {
		return BaseHandler{}, errorx.IllegalArgument.New("rsl.Registry cannot be nil")
	}
	if checker == nil {
		return BaseHandler{}, errorx.IllegalArgument.New("reality.Checker cannot be nil")
	}
	return BaseHandler{StateManager: sm, RSL: reg, Checker: checker}, nil
}

// RefreshRuntimeState pushes user inputs into rsl then force-refreshes cluster
// and the target node state.
func (b BaseHandler) RefreshRuntimeState(
	ctx context.Context,
	target models.TargetType,
	setUserInputsFn func() error,
) error {
	if err := setUserInputsFn(); err != nil {
		return errorx.IllegalState.New("failed to push user inputs into rsl: %v", err)
	}

	ctx1, cancel1 := context.WithTimeout(ctx, rsl.DefaultRefreshTimeout)
	defer cancel1()
	if err := b.RSL.Cluster.RefreshState(ctx1, false); err != nil {
		return errorx.IllegalState.New("failed to refresh cluster state: %v", err)
	}

	// We refresh the target node state with force=false here because we let the reality refresh only if it is stale.
	ctx2, cancel2 := context.WithTimeout(ctx, rsl.DefaultRefreshTimeout)
	defer cancel2()
	switch target {
	case models.TargetBlocknode:
		if err := b.RSL.BlockNode.RefreshState(ctx2, false); err != nil {
			return errorx.IllegalState.New("failed to refresh block node state: %v", err)
		}
	default:
		return errorx.IllegalArgument.New("unsupported target type for runtime refresh: %s", target)
	}

	return nil
}

// FlushNodeState is the exported generic flush used by all node handlers.
// It performs three live refreshes before persisting:
//  1. ClusterState  — via RSL.Cluster.RefreshState  (Helm / k8s queries)
//  2. Target node state — via RSL.BlockNode.RefreshState (Helm release info)
//  3. MachineState  — via Checker.MachineState (binary presence + sidecar files)
//
// All three are written into the full State before the single Flush() call,
// so state.yaml always reflects reality after every workflow execution.
func FlushNodeState[I any](
	ctx context.Context,
	base BaseHandler,
	report *automa.Report,
	intent models.Intent,
	effectiveInputs *models.UserInputs[I],
	patchState func(full *state.State) error,
) (*automa.Report, error) {
	if report == nil {
		return nil, errorx.IllegalArgument.New("workflow report cannot be nil")
	}

	// Skip state persistence when the workflow failed in ContinueOnError mode.
	// In that mode steps may have been skipped or partially applied, so the
	// resulting cluster state is indeterminate — writing it would produce a
	// misleading state.yaml.
	//
	// StopOnError failures are NOT skipped: rollback will have run, and the
	// final state after rollback should still be persisted.
	if report.IsFailed() && effectiveInputs.Common.ExecutionOptions.ExecutionMode == automa.ContinueOnError {
		logx.As().Warn().Msg("workflow execution failed in ContinueOnError mode; skipping state persistence")
		return report, nil
	}

	// refresh state
	err := base.StateManager.Refresh()
	if err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return nil, errorx.IllegalState.New("failed to refresh state manager before flush: %v", err)
	}
	fullState := base.StateManager.State()

	// ── 1. Refresh ClusterState ───────────────────────────────────────────────
	if base.RSL.Cluster == nil {
		return nil, errorx.IllegalState.New("rsl cluster runtime is not initialized")
	}

	ctx1, cancel1 := context.WithTimeout(ctx, rsl.DefaultRefreshTimeout)
	defer cancel1()
	if err := base.RSL.Cluster.RefreshState(ctx1, true); err != nil {
		return nil, errorx.IllegalState.New("failed to refresh cluster state after workflow: %v", err)
	}

	clusterState, err := base.RSL.Cluster.CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to read cluster state after workflow: %v", err)
	}

	// ── 2. Refresh target node state ─────────────────────────────────────────
	ctx2, cancel2 := context.WithTimeout(ctx, rsl.DefaultRefreshTimeout)
	defer cancel2()
	switch intent.Target {
	case models.TargetBlocknode:
		if base.RSL.BlockNode == nil {
			return nil, errorx.IllegalState.New("rsl block node runtime is not initialized")
		}
		if err := base.RSL.BlockNode.RefreshState(ctx2, true); err != nil {
			return nil, errorx.IllegalState.New("failed to refresh block node state after workflow: %v", err)
		}
	default:
		return nil, errorx.IllegalArgument.New("unsupported target type for post-workflow refresh: %s", intent.Target)
	}

	// ── 3. Refresh MachineState (software + hardware) ─────────────────────────
	ctx3, cancel3 := context.WithTimeout(context.Background(), rsl.DefaultRefreshTimeout)
	defer cancel3()

	// This must run after the node-state refresh so that any binary placements
	// made by the workflow steps are visible to the filesystem stat check inside
	// refreshSoftwareState.  We do NOT treat a MachineState refresh error as
	// fatal — a stale MachineState is preferable to losing the whole flush.
	machineState, machineErr := base.Checker.MachineState(ctx3)
	if machineErr != nil {
		logx.As().Warn().Err(machineErr).Msg("failed to refresh MachineState before flush; persisting stale machine state")
		machineState = base.StateManager.State().MachineState // preserve existing
	}

	// ── Assemble full state ───────────────────────────────────────────────────
	fullState.ClusterState = clusterState
	fullState.MachineState = machineState
	if patchState != nil {
		if err = patchState(&fullState); err != nil {
			return nil, err
		}
	}

	base.StateManager.AddActionHistory(state.ActionHistory{
		Intent: intent,
		Inputs: effectiveInputs,
	})

	if err = base.StateManager.Set(fullState).Flush(); err != nil {
		return nil, errorx.IllegalState.New("failed to persist state after workflow: %v", err)
	}

	logx.As().Info().
		Str("state_file", fullState.StateFile).
		Any("full-state", fullState).
		Msg("Persisted state after workflow execution")

	return report, nil
}
