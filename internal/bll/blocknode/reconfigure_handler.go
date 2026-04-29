// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// ReconfigureHandler handles the ActionReconfigure intent for a block node.
// Unlike UpgradeHandler it does not perform any semver comparison — it always
// re-applies values at the currently-deployed chart version.
type ReconfigureHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtime *rsl.BlockNodeRuntimeResolver
}

// PrepareEffectiveInputs resolves fields for a reconfigure.
// The runtime's StrategyCurrent will prefer the deployed ChartVersion because
// reconfigure passes no user-supplied version.
func (h *ReconfigureHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	return resolveBlocknodeEffectiveInputs(h.runtime, intent, inputs, nil)
}

// BuildWorkflow validates reconfigure preconditions and returns the workflow.
// Preconditions:
//   - Block node must already be deployed (or --force).
func (h *ReconfigureHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if currentState.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot reconfigure").
			WithProperty(models.ErrPropertyResolution,
				"use 'weaver block node install' to install the block node first, or pass --force to continue")
	}

	// Fail fast if storage paths can't be resolved.
	if err := bnpkg.ValidateStorageCompleteness(inputs.Custom.Storage, inputs.Custom.ChartVersion); err != nil {
		return nil, err
	}

	ins := inputs.Custom
	var wb *automa.WorkflowBuilder
	if ins.ResetStorage {
		// PurgeBlockNodeStorage scales down, clears storage, and UpgradeBlockNode
		// scales back up via helm — the pod restart is implicit; no extra restart needed.
		wb = automa.NewWorkflowBuilder().WithId("block-node-reconfigure-with-reset").
			Steps(steps.PurgeBlockNodeStorage(ins), steps.UpgradeBlockNode(ins))
	} else if ins.NoRestart {
		// Opt-out: apply new values via helm but skip the rollout-restart.
		// Use when the chart already guarantees pod-spec changes trigger restarts.
		wb = automa.NewWorkflowBuilder().WithId("block-node-reconfigure-no-restart").
			Steps(steps.UpgradeBlockNode(ins))
	} else {
		// Default: apply new values then trigger a rolling restart so ConfigMap-only
		// changes are picked up by the running pod.
		wb = automa.NewWorkflowBuilder().WithId("block-node-reconfigure").
			Steps(steps.UpgradeBlockNode(ins), steps.RolloutRestartBlockNode(ins))
	}
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *ReconfigureHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeChartRef())
}

// NewReconfigureHandler creates a new ReconfigureHandler.
func NewReconfigureHandler(base bll.BaseHandler[models.BlockNodeInputs],
	runtimeState *rsl.BlockNodeRuntimeResolver) (*ReconfigureHandler, error) {
	return &ReconfigureHandler{BaseHandler: base, runtime: runtimeState}, nil
}
