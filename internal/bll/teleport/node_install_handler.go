// SPDX-License-Identifier: Apache-2.0

package teleport

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// NodeInstallHandler handles the ActionInstall intent for the teleport node agent.
type NodeInstallHandler struct {
	bll.BaseHandler[models.TeleportNodeInputs]
	runtime *rsl.TeleportRuntimeResolver
}

func (h *NodeInstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*models.UserInputs[models.TeleportNodeInputs], error) {
	return resolveTeleportNodeEffectiveInputs(h.runtime, intent, inputs)
}

func (h *NodeInstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportNodeInputs],
) (*automa.WorkflowBuilder, error) {
	mr, ok := h.Runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalState.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", h.Runtime.MachineRuntime)
	}
	return steps.SetupTeleportNodeAgent(mr), nil
}

func (h *NodeInstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewNodeInstallHandler(base bll.BaseHandler[models.TeleportNodeInputs], runtime *rsl.TeleportRuntimeResolver) *NodeInstallHandler {
	return &NodeInstallHandler{BaseHandler: base, runtime: runtime}
}
