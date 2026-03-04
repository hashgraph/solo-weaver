// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// InstallHandler handles the ActionInstall intent for a block node.
// It resolves effective inputs (applying RequiresExplicitOverride guards so
// fields already set by a running deployment cannot be silently overridden),
// then builds an install workflow that optionally bootstraps the cluster first.
type InstallHandler struct {
	bll.BaseHandler[models.BlocknodeInputs]
	runtimeState *rsl.BlockNodeRuntimeState
	sm           state.Manager
}

func NewInstallHandler(
	base bll.BaseHandler[models.BlocknodeInputs],
	runtimeState *rsl.BlockNodeRuntimeState,
	sm state.Manager) *InstallHandler {
	return &InstallHandler{BaseHandler: base, runtimeState: runtimeState, sm: sm}
}

// PrepareEffectiveInputs resolves the winning value for every block-node field.
// For each field the priority is: StrategyCurrent > StrategyUserInput > StrategyConfig.
// RequiresExplicitOverride fires when the user supplied a value but the current
// deployed state already owns that field and --force is set — preventing silent
// overwrites during a plain install.
func (h *InstallHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*models.UserInputs[models.BlocknodeInputs], error) {
	return prepareBlocknodeEffectiveInputs(h.runtimeState, inputs, nil)
}

// BuildWorkflow validates install preconditions and returns the workflow.
// If the cluster has already been created only the block node setup step is
// included; otherwise the full cluster bootstrap is prepended.
func (h *InstallHandler) BuildWorkflow(
	currentState state.State,
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*automa.WorkflowBuilder, error) {
	if currentState.BlockNodeState.ReleaseInfo.Status == release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is already installed; cannot install again").
			WithProperty(bll.ErrPropertyResolution,
				"use 'weaver block node reset' or 'weaver block node upgrade', or pass --force to continue")
	}

	ins := inputs.Custom
	var wb *automa.WorkflowBuilder
	if currentState.ClusterState.Created {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").
			Steps(steps.SetupBlockNode(ins))
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").
			Steps(
				workflows.InstallClusterWorkflow(models.NodeTypeBlock, ins.Profile, ins.SkipHardwareChecks, h.sm),
				steps.SetupBlockNode(ins),
			)
	}
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *InstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlocknodeInputs],
) (*automa.Report, error) {
	// Delegate to the shared handler which orchestrates all block-node intents.
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, injectChartRef(h.Runtime, inputs.Custom.Chart))
}
