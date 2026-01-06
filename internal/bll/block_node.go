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

// IntentHandler processes the given intent and user inputs, returning a workflow builder or an error.
func (b BlockNodeIntentHandler) IntentHandler(intent core.Intent, inputs core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, error) {
	validatedIntent, validatedInputs, err := b.prepare(intent, inputs)
	if err != nil {
		return nil, err
	}

	switch validatedIntent.Action {
	case core.ActionInstall:
		wfBuilder, err := b.installHandler(validatedIntent, validatedInputs)
		if err != nil {
			return nil, err
		}
		return wfBuilder, nil

	default:
		return nil, errorx.IllegalArgument.New("unsupported action '%s' for block node", validatedIntent.Action)
	}
}

// prepare performs validation and preparation of intent and inputs.
func (b BlockNodeIntentHandler) prepare(intent core.Intent, inputs core.UserInputs[core.BlocknodeInputs]) (*core.Intent, *core.UserInputs[core.BlocknodeInputs], error) {
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
	err = runtime.BlockNode().SetRelease(inputs.Custom.Release)
	if err != nil {
		return nil, nil, errorx.IllegalState.New("failed to use block node release name as user input: %v", err)
	}
	err = runtime.BlockNode().SetChart(inputs.Custom.ChartUrl)
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

func (b BlockNodeIntentHandler) installHandler(intent *core.Intent, inputs *core.UserInputs[core.BlocknodeInputs]) (*automa.WorkflowBuilder, error) {
	// intent: block node install --profile <profile> --blocknode-version v0.3 --storage-path /data/blocknode
	// inputs: version: v0.3, storage-path: /data/blocknode
	// current: version: v0.1 (on disk), storage-path: /mnt/fast-storage
	// reality: version: v0.1, storage-path: /mnt/fast-storage
	// allowed: NO
	//	- v.0.1 -> v.0.3: NO (version jump isn't allowed, needs to go through v0.2)
	//	- /mnt/fast-storage -> /data/blocknode: YES
	// effective:
	//    - version: v0.2
	//    - storage-path: /data/blocknode

	if runtime.BlockNode().CurrentState().ReleaseInfo.Status == release.StatusDeployed && inputs.Common.Force != true {
		return nil, errorx.IllegalState.New("block node is already installed; cannot install again").
			WithProperty(doctor.ErrPropertyResolution, "use 'weaver block-node upgrade' to upgrade the block node or use --force to continue")
	}

	// Determine Blocknode version
	effVersion, err := runtime.BlockNode().Version()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get effective block node version: %v", err)
	}

	logx.As().Debug().
		Str("inputVersion", inputs.Custom.Version).
		Str("effectiveVersion", effVersion.Get().Val()).
		Str("strategy", effVersion.Strategy().String()).
		Msg("Found effective block node version")

	if effVersion.Strategy() != automa.StrategyUserInput {
		logx.As().Warn().
			Str("inputVersion", inputs.Custom.Version).
			Str("effectiveVersion", effVersion.Get().Val()).
			Str("strategy", effVersion.Strategy().String()).
			Msgf("Overriding block node version from inputs '%s' to effective '%s'",
				inputs.Custom.Version, effVersion.Get().Val())
		inputs.Custom.Version = effVersion.Get().Val()
	}

	// Determine Blocknode namespace
	effNamespace, err := runtime.BlockNode().Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node namespace: %v", err)
	}
	logx.As().Debug().
		Str("inputNamespace", inputs.Custom.Namespace).
		Str("effectiveNamespace", effNamespace.Get().Val()).
		Str("strategy", effNamespace.Strategy().String()).
		Msg("Found effective block node namespace")
	if effNamespace.Strategy() != automa.StrategyUserInput {
		logx.As().Warn().
			Str("inputNamespace", inputs.Custom.Namespace).
			Str("effectiveNamespace", effNamespace.Get().Val()).
			Str("strategy", effNamespace.Strategy().String()).
			Msgf("Overriding block node namespace from inputs '%s' to effective '%s'", inputs.Custom.Namespace, effNamespace.Get().Val())
		inputs.Custom.Namespace = effNamespace.Get().Val()
	}

	// Determine Blocknode release
	effRelease, err := runtime.BlockNode().Release()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node release name: %v", err)
	}
	logx.As().Debug().
		Str("inputRelease", inputs.Custom.Release).
		Str("effectiveRelease", effRelease.Get().Val()).
		Str("strategy", effRelease.Strategy().String()).
		Msg("Found effective block node release name")
	if effRelease.Strategy() != automa.StrategyUserInput {
		logx.As().Warn().
			Str("inputRelease", inputs.Custom.Release).
			Str("effectiveRelease", effRelease.Get().Val()).
			Str("strategy", effRelease.Strategy().String()).
			Msgf("Overriding block node release name from inputs '%s' to effective '%s'", inputs.Custom.Release, effRelease.Get().Val())
		inputs.Custom.Release = effRelease.Get().Val()
	}

	// Determine Blocknode chart name
	effChart, err := runtime.BlockNode().Chart()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart: %v", err)
	}
	logx.As().Debug().
		Str("inputChart", inputs.Custom.ChartUrl).
		Str("effectiveChart", effChart.Get().Val()).
		Str("strategy", effChart.Strategy().String()).
		Msg("Found effective block node chart")
	if effChart.Strategy() != automa.StrategyUserInput {
		logx.As().Warn().
			Str("inputChart", inputs.Custom.ChartUrl).
			Str("effectiveChart", effChart.Get().Val()).
			Str("strategy", effChart.Strategy().String()).
			Msgf("Overriding block node chart from inputs '%s' to effective '%s'", inputs.Custom.ChartUrl, effChart.Get().Val())
		inputs.Custom.ChartUrl = effChart.Get().Val()
	}

	// Determine Blocknode chart version
	effChartVersion, err := runtime.BlockNode().ChartVersion()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node chart version: %v", err)
	}
	logx.As().Debug().
		Str("inputChartVersion", inputs.Custom.ChartVersion).
		Str("effectiveChartVersion", effChartVersion.Get().Val()).
		Str("strategy", effChartVersion.Strategy().String()).
		Msg("Found effective block node chart version")
	if effChartVersion.Strategy() != automa.StrategyUserInput {
		logx.As().Warn().
			Str("inputChartVersion", inputs.Custom.ChartVersion).
			Str("effectiveChartVersion", effChartVersion.Get().Val()).
			Str("strategy", effChartVersion.Strategy().String()).
			Msgf("Overriding block node chart version from inputs '%s' to effective '%s'", inputs.Custom.ChartVersion, effChartVersion.Get().Val())
		inputs.Custom.ChartVersion = effChartVersion.Get().Val()
	}

	// Determine Blocknode storage paths
	effStorage, err := runtime.BlockNode().Storage()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to get current block node storage: %v", err)
	}
	logx.As().Debug().
		Any("inputStorage", inputs.Custom.Storage).
		Any("effectiveStorage", effStorage.Get().Val()).
		Str("strategy", effStorage.Strategy().String()).
		Msg("Found effective block node storage")
	if effStorage.Strategy() != automa.StrategyUserInput {
		logx.As().Warn().
			Any("inputStorage", inputs.Custom.Storage).
			Any("effectiveStorage", effStorage.Get().Val()).
			Str("strategy", effStorage.Strategy().String()).
			Msgf("Overriding block node storage from inputs '%v' to effective '%v'", inputs.Custom.Storage, effStorage.Get().Val())
		inputs.Custom.Storage = effStorage.Get().Val()
	}

	// Build workflow
	var wb *automa.WorkflowBuilder
	if runtime.Cluster().CurrentState().Created {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
			steps.SetupBlockNode(&inputs.Custom),
		)
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
			workflows.InstallClusterWorkflow(core.NodeTypeBlock, inputs.Custom.Profile),
			steps.SetupBlockNode(&inputs.Custom),
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

func NewBlockNodeIntentHandler(conf config.BlockNodeConfig, opts ...Option[BlockNodeIntentHandler]) (*BlockNodeIntentHandler, error) {
	bn := &BlockNodeIntentHandler{conf: conf}

	for _, opt := range opts {
		if err := opt(bn); err != nil {
			return nil, err
		}
	}

	return bn, nil
}

func InitBlockNodeIntentHandler(conf config.BlockNodeConfig, opts ...Option[BlockNodeIntentHandler]) (*BlockNodeIntentHandler, error) {
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
