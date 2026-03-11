// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/Masterminds/semver/v3"
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// UpgradeHandler handles the ActionUpgrade intent for a block node.
// Unlike InstallHandler, it does NOT apply RequiresExplicitOverride guards —
// the whole point of an upgrade is to change version/chart fields.  Instead it
// enforces chart immutability (you cannot switch charts during an upgrade) and
// prevents version downgrade.
type UpgradeHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtimeState rsl.BlockNodeRuntimeResolver
}

func NewUpgradeHandler(base bll.BaseHandler[models.BlockNodeInputs], runtimeState rsl.BlockNodeRuntimeResolver) (*UpgradeHandler, error) {
	return &UpgradeHandler{BaseHandler: base, runtimeState: runtimeState}, nil
}

// PrepareEffectiveInputs resolves fields for an upgrade.
// Chart immutability and semver constraints are enforced inside BuildWorkflow so that all
// precondition errors are reported together after resolution succeeds.
func (h *UpgradeHandler) PrepareEffectiveInputs(
	inputs models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	return resolveBlocknodeEffectiveInputs(h.runtimeState, inputs, nil)
}

// BuildWorkflow validates upgrade preconditions and returns the workflow.
// Preconditions:
//   - Block node must already be deployed.
//   - Chart repository cannot be changed during an upgrade.
//   - Version cannot be downgraded.
func (h *UpgradeHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if currentState.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot upgrade").
			WithProperty(bll.ErrPropertyResolution, "use 'weaver block node install' to install the block node first, or pass --force to continue")
	}

	if currentState.BlockNodeState.ReleaseInfo.ChartRef != inputs.Custom.Chart {
		return nil, errorx.IllegalState.New(
			"block node chart is already set to %q; chart cannot be changed during an upgrade",
			currentState.BlockNodeState.ReleaseInfo.ChartRef).
			WithProperty(bll.ErrPropertyResolution, "use 'weaver block node reset' then 'weaver block node install'")
	}

	currentVer, err := semver.NewVersion(currentState.BlockNodeState.ReleaseInfo.Version)
	if err != nil {
		return nil, errorx.IllegalState.New(
			"failed to parse current block node version %q: %v", currentState.BlockNodeState.ReleaseInfo.Version, err)
	}
	desiredVer, err := semver.NewVersion(inputs.Custom.Version)
	if err != nil {
		return nil, errorx.IllegalState.New(
			"failed to parse desired block node version %q: %v", inputs.Custom.Version, err)
	}
	if desiredVer.LessThan(currentVer) {
		return nil, errorx.IllegalArgument.New(
			"block node version cannot be downgraded from %q to %q",
			currentVer, desiredVer)
	}
	if desiredVer.Equal(currentVer) && !inputs.Common.Force {
		return nil, errorx.IllegalArgument.New(
			"block node is already at version %q; use --force to re-apply", currentVer)
	}

	ins := inputs.Custom
	var wb *automa.WorkflowBuilder
	if ins.ResetStorage {
		wb = automa.NewWorkflowBuilder().WithId("block-node-upgrade-with-reset").
			Steps(steps.PurgeBlockNodeStorage(ins), steps.UpgradeBlockNode(ins))
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-upgrade").
			Steps(steps.UpgradeBlockNode(ins))
	}
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *UpgradeHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	// Delegate to the shared handler which orchestrates all block-node intents.
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, injectChartRef())
}
