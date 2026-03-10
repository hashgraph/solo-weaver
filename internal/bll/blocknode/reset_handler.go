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
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// ResetHandler handles the ActionReset intent for a block node.
// Reset tears down and re-creates the block node in place; it requires the
// node to already be deployed.  Effective-input resolution is a no-op (the
// effective values from the current state are used as-is).
type ResetHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtimeState *rsl.BlockNodeRuntimeResolver
}

func NewResetHandler(base bll.BaseHandler[models.BlockNodeInputs], runtimeState *rsl.BlockNodeRuntimeResolver) *ResetHandler {
	return &ResetHandler{BaseHandler: base, runtimeState: runtimeState}
}

// PrepareEffectiveInputs for reset simply passes inputs through unchanged.
// All field values are taken from the current state — no resolution is needed.
func (h *ResetHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	// reset has no special validation; pass nil validator
	return resolveBlocknodeEffectiveInputs(h.runtimeState, inputs, nil)
}

// BuildWorkflow validates that the block node is deployed and returns the
// reset workflow.
func (h *ResetHandler) BuildWorkflow(
	currentState state.State,
	inputs *models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if currentState.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot reset").
			WithProperty(bll.ErrPropertyResolution, "use 'weaver block node install' to install the block node first, or pass --force to continue")
	}

	wb := automa.NewWorkflowBuilder().WithId("block-node-reset").
		Steps(steps.ResetBlockNode(inputs.Custom))
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *ResetHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	// Delegate to the shared handler which orchestrates all block-node intents.
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, func(st *state.State) error {
		return injectChartRef(inputs.Custom, &st.BlockNodeState)
	})
}
