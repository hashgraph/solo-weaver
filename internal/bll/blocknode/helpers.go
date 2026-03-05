// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// injectChartRef returns the patchState function for block-node flushes.
// It allows patching the BlockNodeState with workflow-specific values (e.g. ChartRef) that are not live-refreshed from
// the cluster or machine.
func injectChartRef(runtime *rsl.RuntimeResolver, chartRef string) func(*state.State) error {
	return func(full *state.State) error {
		if runtime == nil || runtime.BlockNodeRuntime == nil {
			return errorx.IllegalState.New("block node runtime is not available; cannot inject chart ref into state")
		}

		bnState, err := runtime.BlockNodeRuntime.CurrentState()
		if err != nil {
			return errorx.IllegalState.New("failed to read block node state after workflow: %v", err)
		}

		if bnState.ReleaseInfo.Status == release.StatusDeployed {
			bnState.ReleaseInfo.ChartRef = chartRef
		}

		full.BlockNodeState = bnState
		return nil
	}
}

// resolveBlocknodeEffectiveInputs resolves common fields for blocknode commands.
func resolveBlocknodeEffectiveInputs(
	runtimeState *rsl.BlockNodeRuntimeResolver,
	inputs *models.UserInputs[models.BlocknodeInputs],
	validator func(*models.UserInputs[models.BlocknodeInputs]) error,
) (*models.UserInputs[models.BlocknodeInputs], error) {
	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}

	if runtimeState == nil {
		return nil, errorx.IllegalArgument.New("block node runtime state cannot be nil")
	}

	// Set user inputs on the runtime state so they can be accessed by resolver strategies.
	runtimeState.WithUserInputs(inputs.Custom)

	effReleaseName, err := runtimeState.ReleaseName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node release name: %v", err)
	}
	logx.As().Debug().Str("releaseName", effReleaseName.Get().Val()).
		Str("strategy", effReleaseName.Strategy().String()).
		Msg("Determined effective block node release name")

	effVersion, err := runtimeState.Version()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node version: %v", err)
	}
	logx.As().Debug().Str("version", effVersion.Get().Val()).
		Str("strategy", effVersion.Strategy().String()).
		Msg("Determined effective block node version")

	effNamespace, err := runtimeState.Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node namespace: %v", err)
	}
	logx.As().Debug().Str("namespace", effNamespace.Get().Val()).
		Str("strategy", effNamespace.Strategy().String()).
		Msg("Determined effective block node namespace")

	effChartName, err := runtimeState.ChartName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart name: %v", err)
	}
	logx.As().Debug().Str("chartName", effChartName.Get().Val()).
		Str("strategy", effChartName.Strategy().String()).
		Msg("Determined effective block node chart name")

	effChartRepo, err := runtimeState.ChartRepo()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart repo: %v", err)
	}
	logx.As().Debug().Str("chartRepo", effChartRepo.Get().Val()).
		Str("strategy", effChartRepo.Strategy().String()).
		Msg("Determined effective block node chart repo")

	effChartVersion, err := runtimeState.ChartVersion()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart version: %v", err)
	}
	logx.As().Debug().Str("chartVersion", effChartVersion.Get().Val()).
		Str("strategy", effChartVersion.Strategy().String()).
		Msg("Determined effective block node chart version")

	effStorage, err := runtimeState.Storage()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node storage: %v", err)
	}

	logx.As().Debug().
		Any("storage", effChartVersion.Get().Val()).
		Str("strategy", effChartVersion.Strategy().String()).
		Msg("Determined effective block node storage")

	effectiveInputs := models.UserInputs[models.BlocknodeInputs]{
		Common: inputs.Common,
		Custom: models.BlocknodeInputs{
			// Resolved via resolver
			Profile:      inputs.Custom.Profile,
			Release:      effReleaseName.Get().Val(),
			Version:      effVersion.Get().Val(),
			Namespace:    effNamespace.Get().Val(),
			ChartName:    effChartName.Get().Val(),
			Chart:        effChartRepo.Get().Val(),
			ChartVersion: effChartVersion.Get().Val(),
			Storage:      effStorage.Get().Val(),
			// Passed through from user input (no resolution)
			ValuesFile:         inputs.Custom.ValuesFile,
			ReuseValues:        inputs.Custom.ReuseValues,
			SkipHardwareChecks: inputs.Custom.SkipHardwareChecks,
			ResetStorage:       inputs.Custom.ResetStorage,
		},
	}

	if validator != nil {
		if err := validator(&effectiveInputs); err != nil {
			return nil, err
		}
	}

	logx.As().Debug().Any("effectiveUserInputs", effectiveInputs).
		Msg("Determined effective user inputs for block node")

	return &effectiveInputs, nil
}
