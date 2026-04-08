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

// NodeInstallHandler handles the ActionInstall intent for the teleport node agent.
type NodeInstallHandler struct {
	bll.BaseHandler[models.TeleportNodeInputs]
	sm state.Manager
}

func (h *NodeInstallHandler) PrepareEffectiveInputs(
	_ models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*models.UserInputs[models.TeleportNodeInputs], error) {
	return &inputs, nil
}

func (h *NodeInstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportNodeInputs],
) (*automa.WorkflowBuilder, error) {
	return steps.SetupTeleportNodeAgent(h.sm), nil
}

func (h *NodeInstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewNodeInstallHandler(base bll.BaseHandler[models.TeleportNodeInputs], sm state.Manager) *NodeInstallHandler {
	return &NodeInstallHandler{BaseHandler: base, sm: sm}
}
