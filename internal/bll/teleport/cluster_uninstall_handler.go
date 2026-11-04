// SPDX-License-Identifier: Apache-2.0

package teleport

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ClusterUninstallHandler handles the ActionUninstall intent for the teleport cluster agent.
type ClusterUninstallHandler struct {
	bll.BaseHandler[models.TeleportClusterInputs]
	runtime *rsl.TeleportRuntimeResolver
}

var _ bll.DriftDestroyer[models.TeleportClusterInputs] = (*ClusterUninstallHandler)(nil)

func (h *ClusterUninstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*models.UserInputs[models.TeleportClusterInputs], error) {
	return resolveTeleportClusterEffectiveInputs(h.runtime, intent, inputs)
}

func (h *ClusterUninstallHandler) BuildWorkflow(
	_ state.State,
	_ models.UserInputs[models.TeleportClusterInputs],
) (*automa.WorkflowBuilder, error) {
	return steps.TeardownTeleportClusterAgent(), nil
}

// DestroysDrift reports a change to the cluster agent, which the uninstall
// removes. A node agent change is not its to destroy.
func (h *ClusterUninstallHandler) DestroysDrift(_ models.UserInputs[models.TeleportClusterInputs], c drift.Change) bool {
	return drift.OnTeleportClusterAgent(c)
}

func (h *ClusterUninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

func NewClusterUninstallHandler(base bll.BaseHandler[models.TeleportClusterInputs], runtime *rsl.TeleportRuntimeResolver) *ClusterUninstallHandler {
	return &ClusterUninstallHandler{BaseHandler: base, runtime: runtime}
}
