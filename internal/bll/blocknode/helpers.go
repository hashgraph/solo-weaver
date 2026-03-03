// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/resolver"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// injectChartRef returns the patchState function for block-node flushes.
// It allows patching the BlockNodeState with workflow-specific values (e.g. ChartRef) that are not live-refreshed from
// the cluster or machine.
func injectChartRef(base bll.BaseHandler, chartRef string) func(*state.State) error {
	return func(full *state.State) error {
		bnState, err := base.RSL.BlockNode.CurrentState()
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

// prepareBlocknodeEffectiveInputs resolves common fields for blocknode commands.
// fieldGuards contains per-field guard functions (e.g. RequiresExplicitOverride
// closures) that are evaluated after all fields are resolved; any error from a
// guard is returned immediately.  validator, if non-nil, runs additional
// validations on the fully-assembled effective inputs struct.
func prepareBlocknodeEffectiveInputs(
	runtimeState rslAccessor,
	inputs *models.UserInputs[models.BlocknodeInputs],
	validator func(*models.UserInputs[models.BlocknodeInputs]) error,
	fieldGuards ...func() error,
) (*models.UserInputs[models.BlocknodeInputs], error) {
	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}

	// If runtime state is unavailable, we cannot resolve any fields, so return early with user inputs as-is.  This allows
	// workflows that don't require any resolved fields to proceed even if the block node runtime state is not yet available (e.g. uninstall).
	if runtimeState == nil {
		return inputs, nil
	}

	effReleaseName, err := resolver.Field(func() (*automa.EffectiveValue[string], error) { return runtimeState.ReleaseName() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node release name: %v", err)
	}
	logx.As().Debug().Str("releaseName", effReleaseName.Get().Val()).
		Str("strategy", effReleaseName.Strategy().String()).
		Msg("Determined effective block node release name")

	effVersion, err := resolver.Field(func() (*automa.EffectiveValue[string], error) { return runtimeState.Version() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node version: %v", err)
	}
	logx.As().Debug().Str("version", effVersion.Get().Val()).
		Str("strategy", effVersion.Strategy().String()).
		Msg("Determined effective block node version")

	effNamespace, err := resolver.Field(func() (*automa.EffectiveValue[string], error) { return runtimeState.Namespace() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node namespace: %v", err)
	}
	logx.As().Debug().Str("namespace", effNamespace.Get().Val()).
		Str("strategy", effNamespace.Strategy().String()).
		Msg("Determined effective block node namespace")

	effChartName, err := resolver.Field(func() (*automa.EffectiveValue[string], error) { return runtimeState.ChartName() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart name: %v", err)
	}
	logx.As().Debug().Str("chartName", effChartName.Get().Val()).
		Str("strategy", effChartName.Strategy().String()).
		Msg("Determined effective block node chart name")

	effChartRepo, err := resolver.Field(func() (*automa.EffectiveValue[string], error) { return runtimeState.ChartRepo() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart repo: %v", err)
	}
	logx.As().Debug().Str("chartRepo", effChartRepo.Get().Val()).
		Str("strategy", effChartRepo.Strategy().String()).
		Msg("Determined effective block node chart repo")

	effChartVersion, err := resolver.Field(func() (*automa.EffectiveValue[string], error) { return runtimeState.ChartVersion() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart version: %v", err)
	}
	logx.As().Debug().Str("chartVersion", effChartVersion.Get().Val()).
		Str("strategy", effChartVersion.Strategy().String()).
		Msg("Determined effective block node chart version")

	effStorage, err := resolver.Field(func() (*automa.EffectiveValue[models.BlockNodeStorage], error) { return runtimeState.Storage() })
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node storage: %v", err)
	}

	// Run per-field guards (e.g. RequiresExplicitOverride) now that all
	// effective values and their strategies are available.
	for _, guard := range fieldGuards {
		if err := guard(); err != nil {
			return nil, err
		}
	}

	effective := models.UserInputs[models.BlocknodeInputs]{
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
		if err := validator(&effective); err != nil {
			return nil, err
		}
	}

	logx.As().Debug().Any("effectiveUserInputs", effective).
		Msg("Determined effective user inputs for block node")

	return &effective, nil
}
