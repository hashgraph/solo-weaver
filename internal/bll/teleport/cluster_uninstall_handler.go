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

// ClusterUninstallHandler handles the ActionUninstall intent for the teleport cluster agent.
type ClusterUninstallHandler struct {
	bll.BaseHandler[models.TeleportClusterInputs]
}

func (h *ClusterUninstallHandler) PrepareEffectiveInputs(
	_ models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*models.UserInputs[models.TeleportClusterInputs], error) {
	return &inputs, nil
}

func (h *ClusterUninstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportClusterInputs],
) (*automa.WorkflowBuilder, error) {
	return steps.TeardownTeleportClusterAgent(), nil
}

func (h *ClusterUninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewClusterUninstallHandler(base bll.BaseHandler[models.TeleportClusterInputs]) *ClusterUninstallHandler {
	return &ClusterUninstallHandler{BaseHandler: base}
}
