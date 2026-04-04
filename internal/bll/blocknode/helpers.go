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

// injectChartRef saves the user-supplied chart reference into the runtime state.
func injectChartRef() func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
	return func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
		if st.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed {
			return nil
		}
		if effectiveInputs.Custom.Chart == "" {
			logx.As().Debug().Msg("User did not provide a chart reference; skipping injection into runtime state")
			return nil
		}
		st.BlockNodeState.ReleaseInfo.ChartRef = effectiveInputs.Custom.Chart
		return nil
	}
}

// resolveBlocknodeEffectiveInputs resolves common fields for blocknode commands.
func resolveBlocknodeEffectiveInputs(
	runtime *rsl.BlockNodeRuntimeResolver,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
	validator func(*models.UserInputs[models.BlockNodeInputs]) error,
) (*models.UserInputs[models.BlockNodeInputs], error) {
	// Set user inputs on the runtime state so they can be accessed by resolver strategies.
	runtime.WithIntent(intent).WithUserInputs(inputs.Custom)

	effReleaseName, err := runtime.ReleaseName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node release name: %v", err)
	}
	logx.As().Debug().
		Any("releaseName", effReleaseName).
		Msg("Determined effective block node release name")

	effNamespace, err := runtime.Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node namespace: %v", err)
	}
	logx.As().Debug().
		Any("namespace", effNamespace).
		Msg("Determined effective block node namespace")

	effChartName, err := runtime.ChartName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart name: %v", err)
	}
	logx.As().Debug().
		Any("chartName", effChartName).
		Msg("Determined effective block node chart name")

	effChartRepo, err := runtime.ChartRef()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart repo: %v", err)
	}
	logx.As().Debug().
		Any("chartRepo", effChartRepo).
		Msg("Determined effective block node chart repo")

	effChartVersion, err := runtime.ChartVersion()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart version: %v", err)
	}
	logx.As().Debug().
		Any("chartVersion", effChartVersion).
		Msg("Determined effective block node chart version")

	effStorage, err := runtime.Storage()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node storage: %v", err)
	}
	logx.As().Debug().
		Any("storage", effStorage).
		Msg("Determined effective block node storage")

	effectiveInputs := models.UserInputs[models.BlockNodeInputs]{
		Common: inputs.Common,
		Custom: models.BlockNodeInputs{
			// Resolved via resolver
			Profile:      inputs.Custom.Profile,
			Release:      effReleaseName.Get().Val(),
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
