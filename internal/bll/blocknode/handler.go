// SPDX-License-Identifier: Apache-2.0

// Package blocknode implements the business logic layer for block-node intents.
//
// The routing Handler receives a models.Intent, delegates preparation of
// effective inputs and workflow construction to the appropriate per-action
// handler, executes the workflow, and flushes state to disk.
//
// Extending with a new action: implement ActionHandler[models.BlocknodeInputs]
// in a new file and add a case to Handler.HandleIntent.  No other file changes.
package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Handler is the public entry point for all block-node intents.
// It owns no action-specific logic; it only routes to the right per-action
// handler and calls the shared infrastructure on NodeHandlerBase.
type Handler struct {
	bll.NodeHandlerBase
	install   *InstallHandler
	upgrade   *UpgradeHandler
	reset     *ResetHandler
	uninstall *UninstallHandler
}

// NewHandler constructs a Handler with all four action handlers wired up.
// sm is required and must be pre-loaded via Refresh() by the caller.
func NewHandler(
	sm state.Manager,
	registry *rsl.Registry,
	opts ...bll.Option[Handler],
) (*Handler, error) {
	base, err := bll.NewNodeHandlerBase(sm, registry)
	if err != nil {
		return nil, err
	}

	acc := registryAccessor{bn: registry.BlockNode}
	h := &Handler{
		NodeHandlerBase: base,
		install:         newInstallHandler(acc, sm),
		upgrade:         newUpgradeHandler(acc),
		reset:           newResetHandler(acc),
		uninstall:       newUninstallHandler(acc),
	}

	for _, opt := range opts {
		if err = opt(h); err != nil {
			return nil, err
		}
	}
	return h, nil
}

// HandleIntent is the single call-site for all block-node operations from the
// CLI layer.  It:
//  1. Validates intent and user inputs.
//  2. Refreshes rsl state.
//  3. Delegates to the appropriate action handler.
//  4. Executes the workflow.
//  5. Flushes state to disk.
func (h *Handler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlocknodeInputs],
) (*automa.Report, error) {
	// ── 1. Validate intent ─────────────────────────────────────────────────
	if !intent.IsValid() {
		return nil, errorx.IllegalArgument.New("invalid intent: %v", intent)
	}
	if intent.Target != models.TargetBlocknode {
		return nil, errorx.IllegalArgument.New("invalid intent target: %s", intent.Target)
	}
	if err := inputs.Validate(); err != nil {
		return nil, errorx.IllegalArgument.New("invalid user inputs: %v", err)
	}

	// ── 2. Refresh rsl state ───────────────────────────────────────────────
	if err := h.NodeHandlerBase.RefreshRuntimeState(
		models.TargetBlocknode,
		func() error { return h.RSL.BlockNode.SetUserInputs(inputs.Custom) },
	); err != nil {
		return nil, err
	}

	// ── 3. Route to per-action handler ─────────────────────────────────────
	actionHandler, err := h.handlerFor(intent.Action)
	if err != nil {
		return nil, err
	}

	effectiveInputs, err := actionHandler.PrepareEffectiveInputs(&inputs)
	if err != nil {
		return nil, err
	}

	nodeState, err := h.RSL.BlockNode.CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to read current block node state: %v", err)
	}
	clusterState, err := h.RSL.Cluster.CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to read current cluster state: %v", err)
	}

	wb, err := actionHandler.BuildWorkflow(nodeState, clusterState, effectiveInputs)
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
	return bll.FlushNodeState(
		h.NodeHandlerBase,
		report,
		intent,
		effectiveInputs,
		bll.BlockNodePatchState(h.NodeHandlerBase, effectiveInputs.Custom.Chart),
	)
}

// handlerFor returns the ActionHandler for the given action.
func (h *Handler) handlerFor(
	action models.ActionType,
) (bll.ActionHandler[models.BlocknodeInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.install, nil
	case models.ActionUpgrade:
		return h.upgrade, nil
	case models.ActionReset:
		return h.reset, nil
	case models.ActionUninstall:
		return h.uninstall, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for block node", action)
	}
}
