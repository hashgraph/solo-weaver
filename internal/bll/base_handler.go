// SPDX-License-Identifier: Apache-2.0

package bll

// Package bll (business logic layer) owns intent routing, effective-value
// resolution, and workflow orchestration.  It sits between the CLI commands
// (cmd layer) and the workflow / step layer.
//
// Shared infrastructure lives in BaseHandler so that every per-node-type
// handler (BlockNodeRuntimeResolver, MirrorNode, RelayNode, …) inherits it without duplication.

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
// FlushState without re-implementing them.
//
//	type BlockNodeHandler struct {
//	    BaseHandler
//	    install   *BlockNodeInstallHandler
//	    upgrade   *BlockNodeUpgradeHandler
//	    reset     *BlockNodeResetHandler
//	    uninstall *BlockNodeUninstallHandler
//	}
type BaseHandler[T any] struct {
	Runtime *rsl.RuntimeResolver
}

// NewBaseHandler validates the required dependencies and returns a
// populated BaseHandler.  All fields are required; any nil returns an error.
func NewBaseHandler[T any](reg *rsl.RuntimeResolver) (BaseHandler[T], error) {
	if reg == nil {
		return BaseHandler[T]{}, errorx.IllegalArgument.New("RuntimeResolver cannot be nil")
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
	callback func(full *state.State, effInputs models.UserInputs[T]) error, // optional callback for applying additional mutations to the full state
) (*automa.Report, error) {
	// ── 1. Validate intent and inputs ───────────────────────────────────────────────
	err := h.ValidateIntent(intent, inputs, models.TargetBlockNode)
	if err != nil {
		return nil, err
	}

	// ── 2. Refresh runtime state ───────────────────────────────────────────────
	// We need to refresh runtime before preparing effective inputs
	currentState, err := h.Runtime.Refresh(ctx, true)
	if err != nil {
		return nil, err
	}

	// ── 3. Prepare effective inputs ───────────────────────────────────────────────
	effectiveInputs, err := ac.PrepareEffectiveInputs(intent, inputs)
	if err != nil {
		return nil, err
	}

	wb, err := ac.BuildWorkflow(currentState, *effectiveInputs)
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
	return h.FlushState(
		ctx,
		report,
		intent,
		effectiveInputs,
		callback,
	)
}

// FlushState is the exported generic flush used by all node handlers.
func (h *BaseHandler[T]) FlushState(
	ctx context.Context,
	report *automa.Report,
	intent models.Intent,
	effectiveInputs *models.UserInputs[T],
	callback func(full *state.State, effInputs models.UserInputs[T]) error,
) (*automa.Report, error) {
	if report == nil {
		return nil, errorx.IllegalArgument.New("workflow report cannot be nil")
	}

	h.Runtime.AddActionHistory(state.ActionHistory{
		Intent: intent,
		Inputs: effectiveInputs,
	})

	fullState, err := h.Runtime.Refresh(ctx, true)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to refresh runtime state before flush: %v", err)
	}

	if callback != nil {
		logx.As().Debug().Msg("Applying callback mutations to full state before flush")
		if err := callback(&fullState, *effectiveInputs); err != nil {
			return nil, errorx.IllegalState.New("failed to patch state after workflow execution: %v", err)
		}
		logx.As().Debug().Any("fullState", fullState).Msg("State after applying callback mutations")
	}

	// flush state and action history
	if err := h.Runtime.FlushAll(fullState); err != nil {
		return nil, errorx.IllegalState.New("failed to persist state after workflow: %v", err)
	}

	logx.As().Info().
		Str("state_file", fullState.StateFile).
		Any("full-state", fullState).
		Msg("Persisted state after workflow execution")

	return report, nil
}
