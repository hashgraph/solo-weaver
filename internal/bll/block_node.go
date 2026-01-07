// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/runtime"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

var blockNodeIntentHandlerSingleton *BlockNodeIntentHandler

type BlockNodeIntentHandler struct {
	conf config.BlockNodeConfig
	sm   core.StateManager
}

// prepareRuntime performs validation and prepare runtime with the user inputs
// This function ensures that the intent is valid and the runtime Blocknode is set up with the provided user inputs
func (b BlockNodeIntentHandler) prepareRuntime(
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
	err := runtime.BlockNode().RefreshState(ctx, false) // no need to force refresh here
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to refresh block node state: %v", err)
	}

	// Set user inputs into runtime Blocknode so that we can determine effective values
	err = runtime.BlockNode().SetVersion(inputs.Custom.Version)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node version as user input: %v", err)
	}
	err = runtime.BlockNode().SetNamespace(inputs.Custom.Namespace)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node namespace as user input: %v", err)
	}
	err = runtime.BlockNode().SetReleaseName(inputs.Custom.ReleaseName)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node release name as user input: %v", err)
	}
	err = runtime.BlockNode().SetChartRepo(inputs.Custom.ChartRepo)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node chart as user input: %v", err)
	}
	err = runtime.BlockNode().SetChartVersion(inputs.Custom.ChartVersion)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node chart version as user input: %v", err)
	}
	err = runtime.BlockNode().SetStorage(inputs.Custom.Storage)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node storage as user input: %v", err)
	}

	return &intent, &inputs, nil
}

// prepareEffectiveUserInputsForInstall determines the effective user inputs based on current runtime state and provided inputs.
func (b BlockNodeIntentHandler) prepareEffectiveUserInputsForInstall(
	currentState *core.BlockNodeState,
	inputs *core.UserInputs[core.BlocknodeInputs]) (*core.UserInputs[core.BlocknodeInputs], error) {

	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}

	if currentState == nil {
		return nil, errorx.IllegalArgument.New("current block node state cannot be nil")
	}

	// Determine Blocknode release name
	effReleaseName, err := runtime.BlockNode().ReleaseName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node release: %v", err)
	}

	if inputs.Custom.ReleaseName != "" && effReleaseName.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node release name is already set to '%s'; cannot override", effReleaseName.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode version
	effVersion, err := runtime.BlockNode().Version()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get effective block node version: %v", err)
	}

	if inputs.Custom.Version != "" && effVersion.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node version is already set to '%s'; cannot override", effVersion.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode namespace
	effNamespace, err := runtime.BlockNode().Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node namespace: %v", err)
	}

	if inputs.Custom.Namespace != "" && effNamespace.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node namespace is already set to '%s'; cannot override", effNamespace.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode chart repo
	effChartName, err := runtime.BlockNode().ChartName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart name: %v", err)
	}

	if inputs.Custom.ChartName != "" && effChartName.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node chart name is already set to '%s'; cannot override", effChartName.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode chart repo
	effChartRepo, err := runtime.BlockNode().ChartRepo()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart repo: %v", err)
	}

	if inputs.Custom.ChartRepo != "" && effChartRepo.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node chart repo is already set to '%s'; cannot override", effChartRepo.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode chart version
	effChartVersion, err := runtime.BlockNode().ChartVersion()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart version: %v", err)
	}

	if inputs.Custom.ChartVersion != "" && effChartVersion.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node chart version is already set to '%s'; cannot override", effChartVersion.Get().Val()).
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	// Determine Blocknode storage paths
	effStorage, err := runtime.BlockNode().Storage()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node storage: %v", err)
	}

	if effStorage.Strategy() != automa.StrategyUserInput && inputs.Common.Force {
		return nil, errorx.IllegalState.New("block node storage is already set; cannot override").
			WithProperty(doctor.ErrPropertyResolution, "use `weaver block-node upgrade` to upgrade the block node deployment")
	}

	effectiveUserInputs := core.UserInputs[core.BlocknodeInputs]{
		Common: inputs.Common,
		Custom: core.BlocknodeInputs{
			Profile:      inputs.Custom.Profile,
			ReleaseName:  effReleaseName.Get().Val(),
			Version:      effVersion.Get().Val(),
			Namespace:    effNamespace.Get().Val(),
			ChartName:    effChartName.Get().Val(),
			ChartRepo:    effChartRepo.Get().Val(),
			ChartVersion: effChartVersion.Get().Val(),
			Storage:      effStorage.Get().Val(),
		},
	}

	logx.As().Debug().
		Any("release", effReleaseName).
		Any("namespace", effNamespace).
		Any("version", effVersion).
		Any("chartName", effChartName).
		Any("chartRepo", effChartRepo).
		Any("chartVersion", effChartVersion).
		Any("storage", effStorage).
		Any("effectiveUserInputs", effectiveUserInputs).
		Msg("Determined effective user inputs for block node installation")

	return &effectiveUserInputs, nil
}

func (b BlockNodeIntentHandler) installHandler(
	inputs *core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, *core.UserInputs[core.BlocknodeInputs], error) {
	// intent: block node install --profile <profile> --blocknode-version v0.3 --storage-path /data/blocknode
	// inputs: version: v0.3, storage-path: /data/blocknode
	// current: version: v0.1 (on disk), storage-path: /mnt/fast-storage
	// reality: version: v0.1, storage-path: /mnt/fast-storage
	// allowed: NO

	blockNodeState, err := runtime.BlockNode().CurrentState()
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to get current block node state: %v", err)
	}

	if blockNodeState.ReleaseInfo.Status == release.StatusDeployed && inputs.Common.Force != true {
		return nil, nil, errorx.IllegalState.New("block node is already installed; cannot install again").
			WithProperty(doctor.ErrPropertyResolution, "use 'weaver block-node upgrade' to upgrade the block node or use --force to continue")
	}

	effectiveUserInputs, err := b.prepareEffectiveUserInputsForInstall(blockNodeState, inputs)
	if err != nil {
		return nil, nil, err
	}

	var wb *automa.WorkflowBuilder

	clusterState, err := runtime.Cluster().CurrentState()
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to get current cluster state: %v", err)
	}

	blockNodeInputs := effectiveUserInputs.Custom
	if clusterState.Created {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
			steps.SetupBlockNode(&blockNodeInputs),
		)
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
			workflows.InstallClusterWorkflow(core.NodeTypeBlock, blockNodeInputs.Profile),
			steps.SetupBlockNode(&blockNodeInputs),
		)
	}

	return workflows.WithWorkflowExecutionMode(wb, &inputs.Common.ExecutionOptions), effectiveUserInputs, nil
}

func (b BlockNodeIntentHandler) uninstallHandler(inputs *core.BlocknodeInputs) (*automa.WorkflowBuilder, error) {
	return nil, nil
}

func (b BlockNodeIntentHandler) upgradeHandler(inputs *core.BlocknodeInputs) (*automa.WorkflowBuilder, error) {
	return nil, nil
}

// Workflow returns a workflow builder based on the given intent and user inputs.
// It is the caller's responsibility to build and execute the workflow, and persist any state changes.
// Returns the workflow builder and any error encountered.
// It is better to use HandleIntent() which also refreshes the block node state after execution.
func (b BlockNodeIntentHandler) Workflow(
	intent core.Intent,
	inputs core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, *core.UserInputs[core.BlocknodeInputs], error) {
	validatedIntent, validatedInputs, err := b.prepareRuntime(intent, inputs)
	if err != nil {
		return nil, nil, err
	}

	switch validatedIntent.Action {
	case core.ActionInstall:
		wfBuilder, effectiveUserInputs, err := b.installHandler(validatedInputs)
		if err != nil {
			return nil, nil, err
		}
		return wfBuilder, effectiveUserInputs, nil

	default:
		return nil, nil, errorx.IllegalArgument.New("unsupported action '%s' for block node", validatedIntent.Action)
	}
}

// HandleIntent executes the workflow for the given intent and user inputs.
// It also refreshes the block node state after successful execution.
// Returns the workflow report and any error encountered.
func (b BlockNodeIntentHandler) HandleIntent(
	intent core.Intent,
	inputs core.UserInputs[core.BlocknodeInputs]) (*automa.Report, error) {
	wb, effectiveInputs, err := b.Workflow(intent, inputs)
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
		Msg("Running Block Node workflow for intent")
	report := wf.Execute(context.Background())
	logx.As().Info().
		Any("intent", intent).
		Any("inputs", inputs).
		Any("effectiveInputs", effectiveInputs).
		Msg("Completed Block Node workflow execution for intent")

	return b.flushState(report)
}

// flushState persists the current block node state
func (b BlockNodeIntentHandler) flushState(report *automa.Report) (*automa.Report, error) {
	if report == nil {
		return nil, errorx.IllegalArgument.New("workflow report cannot be nil")
	}

	if report.IsFailed() {
		logx.As().Warn().Msg("Workflow execution failed; skipping block node state persistence")
		return report, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), runtime.DefaultRefreshTimeout)
	defer cancel()
	err := runtime.BlockNode().RefreshState(ctx, true)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to refresh block node state after workflow execution: %v", err)
	}

	// get current block node state
	current, err := runtime.BlockNode().CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node state after workflow execution: %v", err)
	}

	// load full state from disk and persist update block node state
	fullState := b.sm.State()
	fullState.BlockNode = *current
	err = b.sm.Flush()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to persist block node state after workflow execution: %v", err)
	}

	logx.As().Info().Msg("Persisted block node state after workflow execution")
	return report, nil
}

func NewBlockNodeIntentHandler(
	conf config.BlockNodeConfig,
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

func InitBlockNodeIntentHandler(
	conf config.BlockNodeConfig,
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
