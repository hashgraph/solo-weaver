// SPDX-License-Identifier: Apache-2.0

package teleport

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NodeUninstallHandler handles the ActionUninstall intent for the teleport node agent.
type NodeUninstallHandler struct {
	bll.BaseHandler[models.TeleportNodeInputs]
	sm state.Manager
}

func (h *NodeUninstallHandler) PrepareEffectiveInputs(
	_ models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*models.UserInputs[models.TeleportNodeInputs], error) {
	return &inputs, nil
}

func (h *NodeUninstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportNodeInputs],
) (*automa.WorkflowBuilder, error) {
	return steps.TeardownTeleportNodeAgent(h.sm), nil
}

func (h *NodeUninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewNodeUninstallHandler(base bll.BaseHandler[models.TeleportNodeInputs], sm state.Manager) *NodeUninstallHandler {
	return &NodeUninstallHandler{BaseHandler: base, sm: sm}
}
