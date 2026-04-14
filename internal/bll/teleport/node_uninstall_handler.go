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

// NodeUninstallHandler handles the ActionUninstall intent for the teleport node agent.
type NodeUninstallHandler struct {
	bll.BaseHandler[models.TeleportNodeInputs]
	runtime *rsl.TeleportRuntimeResolver
}

func (h *NodeUninstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*models.UserInputs[models.TeleportNodeInputs], error) {
	return resolveTeleportNodeEffectiveInputs(h.runtime, intent, inputs)
}

func (h *NodeUninstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportNodeInputs],
) (*automa.WorkflowBuilder, error) {
	mr, ok := h.Runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalState.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", h.Runtime.MachineRuntime)
	}
	return steps.TeardownTeleportNodeAgent(mr), nil
}

func (h *NodeUninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewNodeUninstallHandler(base bll.BaseHandler[models.TeleportNodeInputs], runtime *rsl.TeleportRuntimeResolver) *NodeUninstallHandler {
	return &NodeUninstallHandler{BaseHandler: base, runtime: runtime}
}
