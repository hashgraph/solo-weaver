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
	// readRelease is nil in unit tests, which then resolve ports from --values alone.
	readRelease releaseValuesReader
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
//   - The chart ref and chart version must match the deployed release.
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

	// Reconfigure re-applies values at the deployed chart and version, and performs
	// none of upgrade's checks — no downgrade guard, no already-at-this-version
	// guard, no chart-switch warning. Neither field has a flag on this command, so
	// the only way either can differ is a config file declaring it, where a stale
	// desired-state declaration is far likelier than a deliberate request to move
	// the release through this command. Refuse instead of silently performing an
	// unguarded upgrade or chart switch. --force does not bypass either check: the
	// remedy is a different command, not a flag.
	//
	// Both guards apply only to a release that is actually deployed. The reality
	// checker fills ReleaseInfo for a failed or superseded release too, but
	// setStateSources drops the state tier for those, so the effective values fall
	// back to the compiled-in defaults — which would trip these guards on the bare
	// `reconfigure --force` that the precondition above points at as the remedy.
	if deployed := currentState.BlockNodeState.ReleaseInfo; deployed.Status == release.StatusDeployed {
		if deployed.ChartVersion != "" && inputs.Custom.ChartVersion != deployed.ChartVersion {
			return nil, errorx.IllegalArgument.New(
				"block node chart version cannot be changed by a reconfigure: deployed %q, requested %q",
				deployed.ChartVersion, inputs.Custom.ChartVersion).
				WithProperty(models.ErrPropertyResolution,
					"use 'solo-provisioner block node upgrade' to move to a different chart version, "+
						"or drop blockNode.version from the config file to reconfigure at the deployed version")
		}

		if deployed.ChartRef != "" && inputs.Custom.Chart != deployed.ChartRef {
			return nil, errorx.IllegalArgument.New(
				"block node chart cannot be changed by a reconfigure: deployed %q, requested %q",
				deployed.ChartRef, inputs.Custom.Chart).
				WithProperty(models.ErrPropertyResolution,
					"use 'solo-provisioner block node upgrade' to move to a different chart, "+
						"or drop blockNode.chart from the config file to reconfigure from the deployed chart")
		}
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

	// networkSteps is the shared network-plane prefix for every branch below.
	// Reconfigure resolves both --firewall-enabled and --traffic-shaping-enabled
	// fresh (in the CLI layer, seeded from the current on-host state), so the
	// convergence assembler is driven by the operator's decision and is allowed to
	// tear a feature down when it is turned off. See networkPlaneSteps.
	networkSteps := networkPlaneSteps(ins, inputs.Common.Force, ins.TrafficShapingEnabled, true, ports)

	// Warn (never block) when a storage path is not on local block storage.
	networkSteps = append([]automa.Builder{steps.BlockNodeStorageMediaStep(ins.Storage, ins.ChartVersion, managesPlugins)}, networkSteps...)

	plan, err := planStorage(currentState, ins)
	if err != nil {
		return nil, err
	}

	// Operator-driven day-2 guard (#1319): refuse to reconfigure a legacy,
	// weaver-helm-managed block node — the operator cannot act on a legacy release.
	// Runs first, before any network or storage work.
	stepList := []automa.Builder{
		steps.PrecheckBlockNodeLegacy(ins, steps.DefaultCapsuleKubeProvider, steps.BlockNodeHelmInstalledFn(ins)),
	}
	stepList = append(stepList, networkSteps...)

	// UpgradeBlockNode patches the HelmCapsule with the freshly-rendered values at
	// the deployed chart version; the operator reconciles the release whenever the
	// values hash changes, which rolls the pod to pick up config changes. So there
	// is no separate rollout-restart step (the operator owns the rollout), and
	// --no-scale-up is handled declaratively via replicaCount: 0 in the values
	// (ComputeValues). --no-restart cannot be honored under the operator model: any
	// values change triggers a reconcile; the flag is accepted but has no effect.
	var workflowId string
	switch {
	case plan.recreate:
		// Wipe the deployed directories, then recreate PVs/PVCs and the directories
		// at the requested paths before applying the chart.
		workflowId = "block-node-reconfigure-purge-storage"
		stepList = append(stepList,
			steps.PurgeBlockNodeStorage(plan.purgeIns),
			steps.RecreateBlockNodeStorage(ins),
			steps.UpgradeBlockNode(ins),
		)
	case ins.ResetStorage:
		// --with-reset wipes data only; PVs/PVCs are preserved.
		workflowId = "block-node-reconfigure-with-reset"
		stepList = append(stepList,
			steps.PurgeBlockNodeStorage(plan.purgeIns),
			steps.UpgradeBlockNode(ins),
		)
	default:
		workflowId = "block-node-reconfigure"
		stepList = append(stepList, steps.UpgradeBlockNode(ins))
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
	return &ReconfigureHandler{BaseHandler: base, runtime: runtimeState, readRelease: readDeployedReleaseValues}, nil
}
