// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"context"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

var blockNodeIntentHandlerSingleton *BlockNodeIntentHandler

type BlockNodeIntentHandler struct {
	conf core.BlockNodeConfig
	sm   core.StateManager
}

// prepareRuntimeState validates the intent, user inputs, and refreshes the block node state before processing further.
func (b BlockNodeIntentHandler) prepareRuntimeState(
	intent core.Intent,
	inputs core.UserInputs[core.BlocknodeInputs]) (*core.Intent, *core.UserInputs[core.BlocknodeInputs], error) {
	// Basic validation of intent
	if !intent.IsValid() {
		return nil, nil, errorx.IllegalArgument.New("invalid intent: %v", intent)
	}

	if intent.Target != core.TargetBlocknode {
		return nil, nil, errorx.IllegalArgument.New("invalid intent target: %s", intent.Target)
	}

	// Validate user inputs
	// This is redundant if already validated at CLI parsing time, but double check here for safety
	if err := inputs.Validate(); err != nil {
		return nil, nil, errorx.IllegalArgument.New("invalid user inputs: %v", err)
	}

	// Refresh Blocknode state before proceeding
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := rsl.BlockNode().RefreshState(ctx, false) // no need to force refresh here
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to refresh block node state: %v", err)
	}

	// Set user inputs into the Blocknode runtime state so that we can determine effective values
	err = rsl.BlockNode().SetVersion(inputs.Custom.Version)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node version as user input: %v", err)
	}
	err = rsl.BlockNode().SetNamespace(inputs.Custom.Namespace)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node namespace as user input: %v", err)
	}
	err = rsl.BlockNode().SetReleaseName(inputs.Custom.Release)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node release name as user input: %v", err)
	}
	err = rsl.BlockNode().SetChartRef(inputs.Custom.Chart)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node chart as user input: %v", err)
	}
	err = rsl.BlockNode().SetChartVersion(inputs.Custom.ChartVersion)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node chart version as user input: %v", err)
	}
	err = rsl.BlockNode().SetStorage(inputs.Custom.Storage)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node storage as user input: %v", err)
	}

	return &intent, &inputs, nil
}

// prepareEffectiveUserInputsForInstall determines the effective user inputs based on current runtime state and provided inputs.
func (b BlockNodeIntentHandler) prepareEffectiveUserInputsForInstall(
	inputs *core.UserInputs[core.BlocknodeInputs]) (*core.UserInputs[core.BlocknodeInputs], error) {

	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}

	// Determine Blocknode release name
	effReleaseName, err := rsl.BlockNode().ReleaseName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node release: %v", err)
	}

	logx.As().Debug().Str("releaseName", effReleaseName.Get().Val()).Str("strategy", effReleaseName.Strategy().String()).
		Msg("Determined effective block node release name")

	if inputs.Custom.Release != "" && effReleaseName.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node release name is already set to '%s'; cannot override", effReleaseName.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode version
	effVersion, err := rsl.BlockNode().Version()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get effective block node version: %v", err)
	}

	logx.As().Debug().Str("version", effVersion.Get().Val()).Str("strategy", effVersion.Strategy().String()).
		Msg("Determined effective block node version")

	if inputs.Custom.Version != "" && effVersion.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node version is already set to '%s'; cannot override", effVersion.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode namespace
	effNamespace, err := rsl.BlockNode().Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node namespace: %v", err)
	}

	logx.As().Debug().Str("namespace", effNamespace.Get().Val()).Str("strategy", effNamespace.Strategy().String()).
		Msg("Determined effective block node namespace")

	if inputs.Custom.Namespace != "" && effNamespace.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node namespace is already set to '%s'; cannot override", effNamespace.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode chart repo
	effChartName, err := rsl.BlockNode().ChartName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart name: %v", err)
	}

	logx.As().Debug().Str("chartName", effChartName.Get().Val()).Str("strategy", effChartName.Strategy().String()).
		Msg("Determined effective block node chart name")

	if inputs.Custom.ChartName != "" && effChartName.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node chart name is already set to '%s'; cannot override", effChartName.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode chart repo
	effChartRepo, err := rsl.BlockNode().ChartRepo()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart repo: %v", err)
	}

	logx.As().Debug().Str("chartRepo", effChartRepo.Get().Val()).Str("strategy", effChartRepo.Strategy().String()).
		Msg("Determined effective block node chart repo")

	if inputs.Custom.Chart != "" && effChartRepo.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node chart repo is already set to '%s'; cannot override", effChartRepo.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode chart version
	effChartVersion, err := rsl.BlockNode().ChartVersion()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart version: %v", err)
	}

	logx.As().Debug().Str("chartVersion", effChartVersion.Get().Val()).Str("strategy", effChartVersion.Strategy().String()).
		Msg("Determined effective block node chart version")

	if inputs.Custom.ChartVersion != "" && effChartVersion.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node chart version is already set to '%s'; cannot override", effChartVersion.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode storage paths
	effStorage, err := rsl.BlockNode().Storage()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node storage: %v", err)
	}

	logx.As().Debug().Any("storage", effStorage.Get().Val()).Str("strategy", effStorage.Strategy().String()).
		Msg("Determined effective block node storage")

	if effStorage.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node storage is already set; cannot override").
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	effectiveUserInputs := core.UserInputs[core.BlocknodeInputs]{
		Common: inputs.Common,
		Custom: core.BlocknodeInputs{
			Profile:      inputs.Custom.Profile,
			Release:      effReleaseName.Get().Val(),
			Version:      effVersion.Get().Val(),
			Namespace:    effNamespace.Get().Val(),
			ChartName:    effChartName.Get().Val(),
			Chart:        effChartRepo.Get().Val(),
			ChartVersion: effChartVersion.Get().Val(),
			Storage:      effStorage.Get().Val(),
		},
	}

	logx.As().Debug().
		Any("effectiveUserInputs", effectiveUserInputs).
		Msg("Determined effective user inputs for block node installation")

	return &effectiveUserInputs, nil
}

// prepareWorkflow returns a workflow builder based on the given intent and user inputs.
// It is the caller's responsibility to build and execute the workflow and persist any state changes.
// Returns the workflow builder and any error encountered.
// It is better to use HandleIntent() which also refreshes the block node state after execution.
func (b BlockNodeIntentHandler) prepareWorkflow(
	intent core.Intent,
	inputs core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, *core.UserInputs[core.BlocknodeInputs], error) {
	validatedIntent, validatedInputs, err := b.prepareRuntimeState(intent, inputs)
	if err != nil {
		return nil, nil, err
	}

	blockNodeState, err := rsl.BlockNode().CurrentState()
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to get current block node state: %v", err)
	}

	effectiveUserInputs, err := b.prepareEffectiveUserInputsForInstall(validatedInputs)
	if err != nil {
		return nil, nil, err
	}

	clusterState, err := rsl.Cluster().CurrentState()
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to get current cluster state: %v", err)
	}

	// prepare a workflow builder based on the action
	var wb *automa.WorkflowBuilder
	blockNodeInputs := effectiveUserInputs.Custom // make a copy for readability
	switch validatedIntent.Action {
	case core.ActionInstall:
		if blockNodeState.ReleaseInfo.Status == release.StatusDeployed && inputs.Common.Force != true {
			return nil, nil, errorx.IllegalState.New("block node is already installed; cannot install again").
				WithProperty(doctor.ErrPropertyResolution, "use 'solo-provisioner block-node reset' or 'weaver block-node upgrade' to reset or upgrade respectively or use --force to attempt to continue")
		}

		// if the cluster is already created, we can skip the cluster installation workflow and just run block node setup workflow;
		// otherwise we need to run the full cluster installation workflow which includes block node installation
		if clusterState.Created {
			wb = automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
				steps.SetupBlockNode(blockNodeInputs),
			)
		} else {
			wb = automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
				workflows.InstallClusterWorkflow(core.NodeTypeBlock, blockNodeInputs.Profile, blockNodeInputs.SkipHardwareChecks),
				steps.SetupBlockNode(blockNodeInputs),
			)
		}
		return wb, effectiveUserInputs, nil
	case core.ActionReset:
		if blockNodeState.ReleaseInfo.Status != release.StatusDeployed {
			return nil, nil, errorx.IllegalState.New("block node is not installed; cannot reset").
				WithProperty(doctor.ErrPropertyResolution, "use 'solo-provisioner block-node install' to install the block node")
		}

		wb = automa.NewWorkflowBuilder().WithId("block-node-reset").Steps(
			steps.ResetBlockNode(blockNodeInputs),
		)
		return wb, effectiveUserInputs, nil
	case core.ActionUpgrade:
		if blockNodeState.ReleaseInfo.Status != release.StatusDeployed {
			return nil, nil, errorx.IllegalState.New("block node is not installed; cannot upgrade").
				WithProperty(doctor.ErrPropertyResolution, "use 'solo-provisioner block-node install' to install the block node")
		}

		if blockNodeState.ReleaseInfo.ChartRef != effectiveUserInputs.Custom.Chart {
			return nil, nil, errorx.IllegalState.New("block node chart is already set to '%s'; cannot override", blockNodeState.ReleaseInfo.ChartRef).
				WithProperty(doctor.ErrPropertyResolution, "use `solo-provisioner block-node upgrade` to upgrade the block node deployment")
		}

		currentVersion, err := semver.NewVersion(blockNodeState.ReleaseInfo.Version)
		if err != nil {
			return nil, nil, errorx.IllegalState.New("failed to parse block node version '%s': %v", blockNodeState.ReleaseInfo.Version, err)
		}
		desiredVersion, err := semver.NewVersion(effectiveUserInputs.Custom.Version)
		if err != nil {
			return nil, nil, errorx.IllegalState.New("failed to parse block node version '%s': %v", effectiveUserInputs.Custom.Version, err)
		}
		if desiredVersion.LessThan(currentVersion) {
			return nil, nil, errorx.IllegalArgument.New("block node version cannot be downgraded; current version is '%s'", blockNodeState.ReleaseInfo.Version)
		}

		if blockNodeInputs.ResetStorage {
			wb = automa.NewWorkflowBuilder().WithId("block-node-upgrade-with-reset").Steps(
				steps.PurgeBlockNodeStorage(blockNodeInputs),
				steps.UpgradeBlockNode(blockNodeInputs),
			)
		} else {
			wb = automa.NewWorkflowBuilder().WithId("block-node-upgrade").Steps(
				steps.UpgradeBlockNode(blockNodeInputs),
			)
		}
		return wb, effectiveUserInputs, nil
	default:
		return nil, nil, errorx.IllegalArgument.New("unsupported action '%s' for block node", validatedIntent.Action)
	}
}

// flushState flush current block node state into disk and refresh the state from disk.
func (b BlockNodeIntentHandler) flushState(report *automa.Report, effectiveInputs *core.UserInputs[core.BlocknodeInputs]) (*automa.Report, error) {
	if report == nil {
		return nil, errorx.IllegalArgument.New("workflow report cannot be nil")
	}

	if report.IsFailed() && effectiveInputs.Common.ExecutionOptions.ExecutionMode != automa.StopOnError {
		logx.As().Warn().Msg("prepareWorkflow execution failed; skipping block node state persistence")
		return report, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rsl.DefaultRefreshTimeout)
	defer cancel()
	err := rsl.BlockNode().RefreshState(ctx, true)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to refresh block node state after workflow execution: %v", err)
	}

	// get current block node state
	current, err := rsl.BlockNode().CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node state after workflow execution: %v", err)
	}

	// chartRepo from user inputs since it is not available in the helm release info in the cluster
	current.ReleaseInfo.ChartRef = effectiveInputs.Custom.Chart

	// load full state from disk and persist update block node state
	fullState := b.sm.State()
	fullState.BlockNode = *current
	err = b.sm.Flush()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to persist block node state after workflow execution: %v", err)
	}

	logx.As().Info().Any("full-state", fullState).Msg("Persisted state after workflow execution")
	return report, nil
}

// HandleIntent executes the workflow for the given intent and user inputs.
// It also refreshes the block node state after successful execution.
// Returns the workflow report and any error encountered.
func (b BlockNodeIntentHandler) HandleIntent(
	intent core.Intent,
	inputs core.UserInputs[core.BlocknodeInputs]) (*automa.Report, error) {
	wb, effectiveInputs, err := b.prepareWorkflow(intent, inputs)
	if err != nil {
		return nil, err
	}

	wf, err := wb.Build()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to build workflow: %v", err)
	}

	logx.As().Info().
		Any("intent", intent).
		Any("inputs", inputs).
		Any("effectiveInputs", effectiveInputs).
		Msgf("Running Block Node workflow for intent %q", intent.Action)
	report := wf.Execute(context.Background())
	logx.As().Info().
		Any("intent", intent).
		Any("inputs", inputs).
		Any("effectiveInputs", effectiveInputs).
		Msg("Completed Block Node workflow execution for intent")

	return b.flushState(report, effectiveInputs)
}

// NewBlockNodeIntentHandler initializes a BlockNodeIntentHandler with the provided configuration, state manager, and options.
// Returns the created BlockNodeIntentHandler or an error if any validation or initialization fails during setup.
func NewBlockNodeIntentHandler(
	conf core.BlockNodeConfig,
	sm core.StateManager,
	opts ...Option[BlockNodeIntentHandler]) (*BlockNodeIntentHandler, error) {
	bn := &BlockNodeIntentHandler{conf: conf, sm: sm}

	for _, opt := range opts {
		if err := opt(bn); err != nil {
			return nil, err
		}
	}

	return bn, nil
}

// InitBlockNodeIntentHandler initializes and returns a singleton BlockNodeIntentHandler instance with given configuration.
// If already initialized, it returns the existing singleton instance.
// Accepts block node configuration, state manager, and optional configuration options.
// Returns the initialized BlockNodeIntentHandler instance or an error if initialization fails.
func InitBlockNodeIntentHandler(
	conf core.BlockNodeConfig,
	sm core.StateManager,
	opts ...Option[BlockNodeIntentHandler]) (*BlockNodeIntentHandler, error) {
	if blockNodeIntentHandlerSingleton != nil {
		return blockNodeIntentHandlerSingleton, nil
	}

	bn, err := NewBlockNodeIntentHandler(conf, sm, opts...)
	if err != nil {
		return nil, err
	}

	blockNodeIntentHandlerSingleton = bn
	return blockNodeIntentHandlerSingleton, nil
}

func BlockNode() *BlockNodeIntentHandler {
	return blockNodeIntentHandlerSingleton
}
