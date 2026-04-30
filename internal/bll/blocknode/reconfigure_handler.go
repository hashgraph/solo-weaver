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
		// Purge data at the currently deployed paths: build a copy of ins that
		// carries the *old* storage configuration so that ResetStorage clears the
		// directories that actually exist on disk. Namespace and release are the
		// same in both old and new inputs, so ScaleStatefulSet / WaitForPodsTerminated
		// are unaffected.
		oldIns := ins
		oldIns.Storage = currentState.BlockNodeState.Storage

		// After purging old dirs, recreate PVs/PVCs and create new directories at
		// the new paths, then upgrade the chart.
		wb = automa.NewWorkflowBuilder().WithId("block-node-reconfigure-with-reset").
			Steps(steps.PurgeBlockNodeStorage(oldIns), steps.RecreateBlockNodeStorage(ins), steps.UpgradeBlockNode(ins))
	} else {
		// For non-reset reconfigures, storage path changes require --with-reset because
		// existing PVs/PVCs cannot be mutated in-place; block with a clear error.
		changed, err := storagePathsChanged(currentState.BlockNodeState.Storage, ins)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to compare storage paths")
		}
		if changed {
			return nil, errorx.IllegalArgument.New(
				"storage paths have changed; PVs/PVCs cannot be updated without clearing existing data").
				WithProperty(models.ErrPropertyResolution,
					"re-run with --with-reset to delete existing PVs/PVCs and recreate them at the new paths")
		}

		if ins.NoRestart {
			// Opt-out: apply new values via helm but skip the rollout-restart.
			wb = automa.NewWorkflowBuilder().WithId("block-node-reconfigure-no-restart").
				Steps(steps.UpgradeBlockNode(ins))
		} else {
			// Default: apply new values then trigger a rolling restart so ConfigMap-only
			// changes are picked up by the running pod.
			wb = automa.NewWorkflowBuilder().WithId("block-node-reconfigure").
				Steps(steps.UpgradeBlockNode(ins), steps.RolloutRestartBlockNode(ins))
		}
	}
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *ReconfigureHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeState())
}

// NewReconfigureHandler creates a new ReconfigureHandler.
func NewReconfigureHandler(base bll.BaseHandler[models.BlockNodeInputs],
	runtimeState *rsl.BlockNodeRuntimeResolver) (*ReconfigureHandler, error) {
	return &ReconfigureHandler{BaseHandler: base, runtime: runtimeState}, nil
}
