// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

type InstallHandler struct {
	base    bll.BaseHandler[models.ClusterInputs]
	runtime *rsl.ClusterRuntimeResolver
	sm      state.Manager
}

func NewInstallHandler(
	base bll.BaseHandler[models.ClusterInputs],
	runtime *rsl.ClusterRuntimeResolver,
	sm state.Manager) *InstallHandler {
	return &InstallHandler{base: base, runtime: runtime, sm: sm}
}

func (h *InstallHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.ClusterInputs],
) (*models.UserInputs[models.ClusterInputs], error) {
	// TODO
	return nil, nil
}

func (h *InstallHandler) BuildWorkflow(
	currentState state.State,
	inputs *models.UserInputs[models.ClusterInputs],
) (*automa.WorkflowBuilder, error) {
	return nil, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *InstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.ClusterInputs],
) (*automa.Report, error) {
	return nil, nil
}
