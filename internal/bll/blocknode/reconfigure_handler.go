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
				"use 'solo-provisioner block node install' to install the block node first, or pass --force to continue")
	}

	// Fail fast if storage paths can't be resolved.
	if err := bnpkg.ValidateStorageCompleteness(inputs.Custom.Storage, inputs.Custom.ChartVersion,
		!bnpkg.EffectivePluginsNamesEmpty(inputs.Custom.PluginList, inputs.Custom.ValuesFile)); err != nil {
		return nil, err
	}

	ins := inputs.Custom

	// Resolve the health/statusz port from the operator's effective --values so the
	// bn-health policy drops the port the BN actually listens on rather than a
	// value baked into solo-weaver; falls back to the chart default when the
	// operator supplies no override. Mirrors the install path.
	healthPort, err := bnpkg.ResolveHealthPort(ins.ValuesFile)
	if err != nil {
		return nil, err
	}

	// networkSteps is the shared network-plane prefix for every branch below.
	// Reconfigure resolves both --firewall-enabled and --traffic-shaping-enabled
	// fresh (in the CLI layer, seeded from the current on-host state), so the
	// convergence assembler is driven by the operator's decision and is allowed to
	// tear a feature down when it is turned off. See networkPlaneSteps.
	networkSteps := networkPlaneSteps(ins, inputs.Common.Force, ins.TrafficShapingEnabled, true, healthPort)

	plan, err := planStorage(currentState, ins)
	if err != nil {
		return nil, err
	}

	var (
		stepList   []automa.Builder
		workflowId string
	)
	switch {
	case plan.recreate:
		// Wipe the deployed directories, then recreate PVs/PVCs and the directories
		// at the requested paths before applying the chart.
		workflowId = "block-node-reconfigure-purge-storage"
		stepList = append(networkSteps,
			steps.PurgeBlockNodeStorage(plan.purgeIns),
			steps.RecreateBlockNodeStorage(ins),
			steps.UpgradeBlockNode(ins),
		)
	case ins.ResetStorage:
		// --with-reset wipes data only; PVs/PVCs are preserved.
		workflowId = "block-node-reconfigure-with-reset"
		stepList = append(networkSteps,
			steps.PurgeBlockNodeStorage(plan.purgeIns),
			steps.UpgradeBlockNode(ins),
		)
	default:
		stepList = append(networkSteps, steps.UpgradeBlockNode(ins))
		switch {
		case ins.NoRestart:
			// Opt-out: apply new values via helm but skip the rollout-restart.
			workflowId = "block-node-reconfigure-no-restart"
		case ins.LeaveScaledDown:
			// A rollout-restart would recreate the pod only for the trailing
			// scale-down to remove it again.
			workflowId = "block-node-reconfigure-scaled-down"
		default:
			// Default: apply new values then trigger a rolling restart so ConfigMap-only
			// changes are picked up by the running pod.
			workflowId = "block-node-reconfigure"
			stepList = append(stepList, steps.RolloutRestartBlockNode(ins))
		}
	}

	// --no-scale-up must be the last word: the branches it can reach all end in
	// a helm upgrade, which re-asserts the chart's replica default.
	if ins.LeaveScaledDown {
		stepList = append(stepList, steps.ScaleDownBlockNodeAfterUpgrade(ins))
	}

	return automa.NewWorkflowBuilder().WithId(workflowId).Steps(stepList...), nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *ReconfigureHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeStateWithTrafficShaping())
}

// NewReconfigureHandler creates a new ReconfigureHandler.
func NewReconfigureHandler(base bll.BaseHandler[models.BlockNodeInputs],
	runtimeState *rsl.BlockNodeRuntimeResolver) (*ReconfigureHandler, error) {
	return &ReconfigureHandler{BaseHandler: base, runtime: runtimeState}, nil
}
