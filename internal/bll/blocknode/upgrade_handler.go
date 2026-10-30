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

// ShapingCleanupDoneStepId is the step ID that records a finished shaping cleanup.
const ShapingCleanupDoneStepId = "shaping-cleanup-done"

// UpgradeHandler handles the ActionUpgrade intent for a block node.
// Unlike InstallHandler, it does NOT apply RequiresExplicitOverride guards —
// the whole point of an upgrade is to change version/chart fields.  Instead it
// enforces chart immutability (you cannot switch charts during an upgrade) and
// prevents version downgrade.
type UpgradeHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtime *rsl.BlockNodeRuntimeResolver
	// readRelease is nil in unit tests, which then resolve ports from --values alone.
	readRelease releaseValuesReader
	// bnPoliciesPresent is nil in unit tests, which then see no live BN policies.
	bnPoliciesPresent func() (bool, error)
	// shapingCleanupDone is set once this run has nothing left to clean up for
	// TrafficShapingCleanupPending, so the flush can clear the mark.
	shapingCleanupDone bool
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
//   - Block node must already be installed (deployed, or failed by the last operation).
//   - Chart repository cannot be changed during an upgrade.
//   - Version cannot be downgraded.
func (h *UpgradeHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if err := requireInstalled(currentState.BlockNodeState.ReleaseInfo, "upgrade", inputs.Common.Force); err != nil {
		return nil, err
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
				"version downgrade is not supported; use 'solo-provisioner block node reconfigure' to re-apply configuration at the current version")
	}
	// Re-running at the version of a failed revision is a retry, not a no-op.
	retry := currentState.BlockNodeState.ReleaseInfo.Status == release.StatusFailed
	if desiredVer.Equal(currentVer) && !inputs.Common.Force && !retry {
		return nil, errorx.IllegalArgument.New(
			"block node is already at chart version %q; use --force to re-apply", currentVer)
	}

	// Fail fast if storage paths can't be resolved.
	managesPlugins := !bnpkg.EffectivePluginsNamesEmpty(inputs.Custom.PluginList, inputs.Custom.ValuesFile)
	if err := bnpkg.ValidateStorageCompleteness(inputs.Custom.Storage, inputs.Custom.ChartVersion, managesPlugins); err != nil {
		return nil, err
	}

	ins := inputs.Custom

	ports, err := resolveBlockNodePorts(ins, h.readRelease)
	if err != nil {
		return nil, err
	}

	// Silent network-plane convergence: re-assert the persisted host-firewall and
	// traffic-shaping decision on every upgrade, create-if-missing only
	// (allowTeardown=false), so a routine version bump neither regresses the network
	// plane nor tears it down. The traffic-shaping target comes from the install-time
	// decision recorded on BlockNodeState — upgrade never prompts and never resolves
	// its own gate (only reconfigure does). Daemon activation, when traffic shaping is
	// enabled, is handled post-workflow in the CLI layer. The one teardown is
	// shaping marked for cleanup, which nobody chose.
	removeShaping := h.planShapingCleanup(currentState.BlockNodeState)
	networkSteps := networkPlaneSteps(ins, inputs.Common.Force, currentState.BlockNodeState.TrafficShapingEnabled, false, removeShaping, ports)
	if removeShaping {
		networkSteps = append(networkSteps, h.shapingCleanupDoneStep())
	}

	// Warn (never block) when a storage path is not on local block storage.
	networkSteps = append([]automa.Builder{steps.BlockNodeStorageMediaStep(ins.Storage, ins.ChartVersion, managesPlugins)}, networkSteps...)

	plan, err := planStorage(currentState, ins)
	if err != nil {
		return nil, err
	}

	stepList := networkSteps
	workflowId := "block-node-upgrade"
	switch {
	case plan.recreate:
		workflowId = "block-node-upgrade-purge-storage"
		stepList = append(stepList,
			steps.PurgeBlockNodeStorage(plan.purgeIns),
			steps.RecreateBlockNodeStorage(ins),
		)
	case ins.ResetStorage:
		workflowId = "block-node-upgrade-with-reset"
		stepList = append(stepList, steps.PurgeBlockNodeStorage(plan.purgeIns))
	}
	stepList = append(stepList, steps.UpgradeBlockNode(ins))

	// --no-scale-up must be the last word: the helm upgrade above re-asserts
	// the chart's replica default.
	if ins.LeaveScaledDown {
		stepList = append(stepList, steps.ScaleDownBlockNodeAfterUpgrade(ins))
	}

	return automa.NewWorkflowBuilder().WithId(workflowId).Steps(stepList...), nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *UpgradeHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	base := patchBlockNodeState()
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h,
		func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
			if h.shapingCleanupDone {
				st.BlockNodeState.TrafficShapingCleanupPending = false
			}
			return base(st, effectiveInputs)
		})
}

// planShapingCleanup reports whether this upgrade must remove traffic shaping
// marked by TrafficShapingCleanupPending. With no BN policies live there is
// nothing to remove, and the mark is cleared without touching the host.
func (h *UpgradeHandler) planShapingCleanup(bn state.BlockNodeState) bool {
	h.shapingCleanupDone = false
	if !bn.TrafficShapingCleanupPending {
		return false
	}
	if bn.TrafficShapingEnabled {
		h.shapingCleanupDone = true
		return false
	}

	present := false
	if h.bnPoliciesPresent != nil {
		var err error
		if present, err = h.bnPoliciesPresent(); err != nil {
			logx.As().Warn().Err(err).Msg(
				"could not check for traffic shaping left on by an earlier upgrade; will check again on the next upgrade")
			return false
		}
	}
	if !present {
		h.shapingCleanupDone = true
		return false
	}

	logx.As().Warn().Msg("Removing traffic shaping that an earlier upgrade turned on without a recorded decision. " +
		"To turn it back on, run 'solo-provisioner block node reconfigure --traffic-shaping-enabled' after the upgrade.")
	return true
}

// shapingCleanupDoneStep runs after the shaping teardown steps, so it is reached
// only when they all succeeded.
func (h *UpgradeHandler) shapingCleanupDoneStep() automa.Builder {
	return automa.NewStepBuilder().WithId(ShapingCleanupDoneStepId).
		WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
			h.shapingCleanupDone = true
			return automa.SuccessReport(stp)
		})
}

func NewUpgradeHandler(base bll.BaseHandler[models.BlockNodeInputs],
	runtimeState *rsl.BlockNodeRuntimeResolver) (*UpgradeHandler, error) {
	return &UpgradeHandler{
		BaseHandler:       base,
		runtime:           runtimeState,
		readRelease:       readDeployedReleaseValues,
		bnPoliciesPresent: steps.BNPoliciesPresent,
	}, nil
}
