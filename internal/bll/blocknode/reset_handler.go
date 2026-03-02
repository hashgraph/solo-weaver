// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// ResetHandler handles the ActionReset intent for a block node.
// Reset tears down and re-creates the block node in place; it requires the
// node to already be deployed.  Effective-input resolution is a no-op (the
// effective values from the current state are used as-is).
type ResetHandler struct {
	base rslAccessor
}

func newResetHandler(base rslAccessor) *ResetHandler {
	return &ResetHandler{base: base}
}

// PrepareEffectiveInputs for reset simply passes inputs through unchanged.
// All field values are taken from the current state — no resolution is needed.
func (h *ResetHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*models.UserInputs[models.BlocknodeInputs], error) {
	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}
	return inputs, nil
}

// BuildWorkflow validates that the block node is deployed and returns the
// reset workflow.
func (h *ResetHandler) BuildWorkflow(
	nodeState state.BlockNodeState,
	_ state.ClusterState,
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*automa.WorkflowBuilder, error) {
	if nodeState.ReleaseInfo.Status != release.StatusDeployed {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot reset").
			WithProperty(errPropertyResolution, "use 'weaver block node install' to install the block node")
	}

	wb := automa.NewWorkflowBuilder().WithId("block-node-reset").
		Steps(resetBlockNode(inputs.Custom))
	return wb, nil
}
