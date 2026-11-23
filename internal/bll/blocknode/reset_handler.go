// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ResetHandler handles the ActionReset intent for a block node.
// Reset tears down and re-creates the block node in place; it requires the
// node to already be deployed.  Effective-input resolution is a no-op (the
// effective values from the current state are used as-is).
type ResetHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtime *rsl.BlockNodeRuntimeResolver
}

// PrepareEffectiveInputs for reset simply passes inputs through unchanged.
// All field values are taken from the current state — no resolution is needed.
func (h *ResetHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	// reset has no special validation; pass nil validator
	return resolveBlocknodeEffectiveInputs(h.runtime, intent, inputs, nil)
}

// BuildWorkflow validates that the block node is installed and returns the
// reset workflow.
func (h *ResetHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if err := requireInstalled(currentState.BlockNodeState.ReleaseInfo, "reset", inputs.Common.Force); err != nil {
		return nil, err
	}

	plan, err := planStorage(currentState, inputs.Custom)
	if err != nil {
		return nil, err
	}

	workflowId := "block-node-reset"
	if plan.recreate {
		workflowId = "block-node-reset-purge-storage"
	}

	wb := automa.NewWorkflowBuilder().WithId(workflowId).
		Steps(steps.ResetBlockNode(plan.purgeIns.Storage, inputs.Custom))
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *ResetHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeState())
}

func NewResetHandler(base bll.BaseHandler[models.BlockNodeInputs], runtimeState *rsl.BlockNodeRuntimeResolver) (*ResetHandler, error) {
	return &ResetHandler{BaseHandler: base, runtime: runtimeState}, nil
}
