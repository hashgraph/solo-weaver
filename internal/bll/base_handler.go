// SPDX-License-Identifier: Apache-2.0

package bll

// Package bll (business logic layer) owns intent routing, effective-value
// resolution, and workflow orchestration.  It sits between the CLI commands
// (cmd layer) and the workflow / step layer.
//
// Shared infrastructure lives in BaseHandler so that every per-node-type
// handler (BlockNodeRuntimeState, MirrorNode, RelayNode, …) inherits it without duplication.

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// ── BaseHandler ───────────────────────────────────────────────────────────

// BaseHandler holds the three dependencies that every node-type handler
// needs: a StateManager for reading and writing state, the rsl Runtime, and
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
type BaseHandler[T any] struct {
	Runtime *rsl.Runtime
}

// NewBaseHandler validates the required dependencies and returns a
// populated BaseHandler.  All fields are required; any nil returns an error.
func NewBaseHandler[T any](reg *rsl.Runtime) (BaseHandler[T], error) {
	if reg == nil {
		return BaseHandler[T]{}, errorx.IllegalArgument.New("rsl.Runtime cannot be nil")
	}
	return BaseHandler[T]{Runtime: reg}, nil
}

func (h *BaseHandler[T]) ValidateIntent(intent models.Intent, inputs models.UserInputs[T], target models.TargetType) error {
	// ── 1. Validate intent ─────────────────────────────────────────────────
	if !intent.IsValid() {
		return errorx.IllegalArgument.New("invalid intent: %v", intent)
	}

	if intent.Target != target {
		return errorx.IllegalArgument.New("intent target mismatch: expected %s, got %s", target, intent.Target)
	}

	if err := inputs.Validate(); err != nil {
		return errorx.IllegalArgument.New("invalid user inputs: %v", err)
	}

	return nil
}

// HandleIntent is the shared generic handler.
// It performs the following steps:
//  1. Validates the intent and user inputs.
//  2. Sets user inputs into the runtime state for effective-value resolution.
//  3. Refreshes the runtime state to ensure it's up-to-date before workflow execution.
//  4. Delegates to the per-action handler to prepare effective inputs and build the workflow, then executes it.
//  5. Flushes the updated state to disk, performing three live refreshes before persistence.
//
// The callback parameter is an optional function that allows per-action handlers to apply additional mutations to the
// full state before it is flushed to disk.
func (h *BaseHandler[T]) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[T],
	ac IntentHandler[T],
	callback func(full *state.State) error, // optional callback for applying additional mutations to the full state
) (*automa.Report, error) {
	// ── 1. Validate intent and inputs ───────────────────────────────────────────────
	err := h.ValidateIntent(intent, inputs, models.TargetBlockNode)
	if err != nil {
		return nil, err
	}

	// ── 2. Set user inputs to runtime state ───────────────────────────────────────────────
	err = h.Runtime.SetUserInputs(intent.Target, inputs.Custom)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to set user inputs into runtime: %v", err)
	}

	// ── 3. Refresh runtime state ───────────────────────────────────────────────
	// We need to refresh before preparing effective inputs
	currentState, err := h.Runtime.Refresh(ctx)
	if err != nil {
		return nil, err
	}

	// ── 4. Prepare effective inputs ───────────────────────────────────────────────
	effectiveInputs, err := ac.PrepareEffectiveInputs(&inputs)
	if err != nil {
		return nil, err
	}

	wb, err := ac.BuildWorkflow(currentState, effectiveInputs)
	if err != nil {
		return nil, err
	}

	// ── 4. Execute workflow ────────────────────────────────────────────────
	wf, err := wb.Build()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to build workflow: %v", err)
	}

	logx.As().Info().
		Any("intent", intent).
		Any("inputs", inputs).
		Any("effectiveInputs", effectiveInputs).
		Msgf("Running block node workflow for intent %q", intent.Action)

	report := wf.Execute(ctx)

	// ── 5. Flush state ─────────────────────────────────────────────────────
	return h.FlushNodeState(
		ctx,
		report,
		intent,
		effectiveInputs,
		callback,
	)
}

// FlushNodeState is the exported generic flush used by all node handlers.
// It performs three live refreshes before persisting:
//  1. ClusterState  — via Runtime.ClusterRuntimeState.RefreshState  (Helm / k8s queries)
//  2. Target node state — via Runtime.BlockNodeRuntimeState.RefreshState (Helm release info)
//  3. MachineState  — via Checker.MachineState (binary presence + sidecar files)
//
// All three are written into the full State before the single Flush() call,
// so state.yaml always reflects reality after every workflow execution.
func (h *BaseHandler[T]) FlushNodeState(
	ctx context.Context,
	report *automa.Report,
	intent models.Intent,
	effectiveInputs *models.UserInputs[T],
	callback func(full *state.State) error,
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

	fullState, err := h.Runtime.Refresh(ctx)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to refresh runtime state before flush: %v", err)
	}

	h.Runtime.AddActionHistory(state.ActionHistory{
		Intent: intent,
		Inputs: effectiveInputs,
	})

	if callback != nil {
		if err := callback(&fullState); err != nil {
			return nil, errorx.IllegalState.New("failed to patch state after workflow execution: %v", err)
		}
	}

	if err := h.Runtime.Flush(fullState); err != nil {
		return nil, errorx.IllegalState.New("failed to persist state after workflow: %v", err)
	}

	logx.As().Info().
		Str("state_file", fullState.StateFile).
		Any("full-state", fullState).
		Msg("Persisted state after workflow execution")

	return report, nil
}
