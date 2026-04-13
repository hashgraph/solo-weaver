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
)

// ClusterInstallHandler handles the ActionInstall intent for the teleport cluster agent.
type ClusterInstallHandler struct {
	bll.BaseHandler[models.TeleportClusterInputs]
	runtime *rsl.TeleportRuntimeResolver
}

func (h *ClusterInstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*models.UserInputs[models.TeleportClusterInputs], error) {
	return resolveTeleportClusterEffectiveInputs(h.runtime, intent, inputs)
}

func (h *ClusterInstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportClusterInputs],
) (*automa.WorkflowBuilder, error) {
	return steps.SetupTeleportClusterAgent(), nil
}

func (h *ClusterInstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewClusterInstallHandler(base bll.BaseHandler[models.TeleportClusterInputs], runtime *rsl.TeleportRuntimeResolver) *ClusterInstallHandler {
	return &ClusterInstallHandler{BaseHandler: base, runtime: runtime}
}
