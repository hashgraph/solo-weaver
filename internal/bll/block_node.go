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
}

// prepareRuntime performs validation and preparation of intent and inputs.
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

	// Refresh Blocknode state before proceeding
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := runtime.BlockNode().RefreshState(ctx)
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
	inputs *core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, error) {
	// intent: block node install --profile <profile> --blocknode-version v0.3 --storage-path /data/blocknode
	// inputs: version: v0.3, storage-path: /data/blocknode
	// current: version: v0.1 (on disk), storage-path: /mnt/fast-storage
	// reality: version: v0.1, storage-path: /mnt/fast-storage
	// allowed: NO

	blockNodeState, err := runtime.BlockNode().CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node state: %v", err)
	}

	if blockNodeState.ReleaseInfo.Status == release.StatusDeployed && inputs.Common.Force != true {
		return nil, errorx.IllegalState.New("block node is already installed; cannot install again").
			WithProperty(doctor.ErrPropertyResolution, "use 'weaver block-node upgrade' to upgrade the block node or use --force to continue")
	}

	effectiveUserInputs, err := b.prepareEffectiveUserInputsForInstall(blockNodeState, inputs)
	if err != nil {
		return nil, err
	}

	var wb *automa.WorkflowBuilder

	clusterState, err := runtime.Cluster().CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current cluster state: %v", err)
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

	return workflows.WithWorkflowExecutionMode(wb, &inputs.Common.ExecutionOptions), nil
}

func (b BlockNodeIntentHandler) uninstallHandler(inputs *core.BlocknodeInputs) (*automa.WorkflowBuilder, error) {
	return nil, nil
}

func (b BlockNodeIntentHandler) upgradeHandler(inputs *core.BlocknodeInputs) (*automa.WorkflowBuilder, error) {
	return nil, nil
}

// IntentHandler processes the given intent and user inputs, returning a workflow builder or an error.
func (b BlockNodeIntentHandler) IntentHandler(
	intent core.Intent,
	inputs core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, error) {
	validatedIntent, validatedInputs, err := b.prepareRuntime(intent, inputs)
	if err != nil {
		return nil, err
	}

	switch validatedIntent.Action {
	case core.ActionInstall:
		wfBuilder, err := b.installHandler(validatedInputs)
		if err != nil {
			return nil, err
		}
		return wfBuilder, nil

	default:
		return nil, errorx.IllegalArgument.New("unsupported action '%s' for block node", validatedIntent.Action)
	}
}

func NewBlockNodeIntentHandler(
	conf config.BlockNodeConfig,
	opts ...Option[BlockNodeIntentHandler]) (*BlockNodeIntentHandler, error) {
	bn := &BlockNodeIntentHandler{conf: conf}

	for _, opt := range opts {
		if err := opt(bn); err != nil {
			return nil, err
		}
	}

	return bn, nil
}

func InitBlockNodeIntentHandler(
	conf config.BlockNodeConfig,
	opts ...Option[BlockNodeIntentHandler]) (*BlockNodeIntentHandler, error) {
	if blockNodeIntentHandlerSingleton != nil {
		return blockNodeIntentHandlerSingleton, nil
	}

	bn, err := NewBlockNodeIntentHandler(conf, opts...)
	if err != nil {
		return nil, err
	}

	blockNodeIntentHandlerSingleton = bn
	return blockNodeIntentHandlerSingleton, nil
}

func BlockNode() *BlockNodeIntentHandler {
	return blockNodeIntentHandlerSingleton
}
