// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/Masterminds/semver/v3"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
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
	runtime *rsl.BlockNodeRuntimeResolver
}

// PrepareEffectiveInputs resolves fields for an upgrade.
// Chart immutability and semver constraints are enforced inside BuildWorkflow so that all
// precondition errors are reported together after resolution succeeds.
func (h *UpgradeHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	return resolveBlocknodeEffectiveInputs(h.runtime, intent, inputs, nil)
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
			WithProperty(models.ErrPropertyResolution, "use 'weaver block node install' to install the block node first, or pass --force to continue")
	}

	if currentState.BlockNodeState.ReleaseInfo.ChartRef != "" &&
		currentState.BlockNodeState.ReleaseInfo.ChartRef != inputs.Custom.Chart {
		logx.As().Warn().
			Str("current_chart", currentState.BlockNodeState.ReleaseInfo.ChartRef).
			Str("desired_chart", inputs.Custom.Chart).
			Msg("Block node chart reference is changing during upgrade; this is not recommended and may cause issues")
	}

	currentVer, err := semver.NewVersion(currentState.BlockNodeState.ReleaseInfo.ChartVersion)
	if err != nil {
		return nil, errorx.IllegalState.New(
			"failed to parse current chart version %q: %v", currentState.BlockNodeState.ReleaseInfo.ChartVersion, err)
	}
	desiredVer, err := semver.NewVersion(inputs.Custom.ChartVersion)
	if err != nil {
		return nil, errorx.IllegalState.New(
			"failed to parse desired chart version %q: %v", inputs.Custom.ChartVersion, err)
	}
	if desiredVer.LessThan(currentVer) {
		return nil, errorx.IllegalArgument.New(
			"block node chart version cannot be downgraded from %q to %q",
			currentVer, desiredVer).
			WithProperty(models.ErrPropertyResolution,
				"version downgrade is not supported; use 'weaver block node reconfigure' to re-apply configuration at the current version")
	}
	if desiredVer.Equal(currentVer) && !inputs.Common.Force {
		return nil, errorx.IllegalArgument.New(
			"block node is already at chart version %q; use --force to re-apply", currentVer)
	}

	// Fail fast if storage paths can't be resolved.
	if err := bnpkg.ValidateStorageCompleteness(inputs.Custom.Storage, inputs.Custom.ChartVersion); err != nil {
		return nil, err
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
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeState())
}

func NewUpgradeHandler(base bll.BaseHandler[models.BlockNodeInputs],
	runtimeState *rsl.BlockNodeRuntimeResolver) (*UpgradeHandler, error) {
	return &UpgradeHandler{BaseHandler: base, runtime: runtimeState}, nil
}
