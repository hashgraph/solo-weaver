// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/resolver"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// InstallHandler handles the ActionInstall intent for a block node.
// It resolves effective inputs (applying RequiresExplicitOverride guards so
// fields already set by a running deployment cannot be silently overridden),
// then builds an install workflow that optionally bootstraps the cluster first.
type InstallHandler struct {
	base rslAccessor
}

func newInstallHandler(base rslAccessor) *InstallHandler {
	return &InstallHandler{base: base}
}

// PrepareEffectiveInputs resolves the winning value for every block-node field.
// For each field the priority is: StrategyCurrent > StrategyUserInput > StrategyConfig.
// RequiresExplicitOverride fires when the user supplied a value but the current
// deployed state already owns that field and --force is set — preventing silent
// overwrites during a plain install.
func (h *InstallHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*models.UserInputs[models.BlocknodeInputs], error) {
	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}

	const hint = "use `weaver block node upgrade` to upgrade the block node deployment"
	force := inputs.Common.Force

	effReleaseName, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ReleaseName() },
		resolver.RequiresExplicitOverride[string]("release", inputs.Custom.Release != "", force, hint),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node release name: %v", err)
	}
	logx.As().Debug().Str("releaseName", effReleaseName.Get().Val()).
		Str("strategy", effReleaseName.Strategy().String()).
		Msg("Determined effective block node release name")

	effVersion, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.Version() },
		resolver.RequiresExplicitOverride[string]("version", inputs.Custom.Version != "", force, hint),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node version: %v", err)
	}
	logx.As().Debug().Str("version", effVersion.Get().Val()).
		Str("strategy", effVersion.Strategy().String()).
		Msg("Determined effective block node version")

	effNamespace, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.Namespace() },
		resolver.RequiresExplicitOverride[string]("namespace", inputs.Custom.Namespace != "", force, hint),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node namespace: %v", err)
	}
	logx.As().Debug().Str("namespace", effNamespace.Get().Val()).
		Str("strategy", effNamespace.Strategy().String()).
		Msg("Determined effective block node namespace")

	effChartName, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ChartName() },
		resolver.RequiresExplicitOverride[string]("chartName", inputs.Custom.ChartName != "", force, hint),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart name: %v", err)
	}
	logx.As().Debug().Str("chartName", effChartName.Get().Val()).
		Str("strategy", effChartName.Strategy().String()).
		Msg("Determined effective block node chart name")

	effChartRepo, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ChartRepo() },
		resolver.RequiresExplicitOverride[string]("chart", inputs.Custom.Chart != "", force, hint),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart repo: %v", err)
	}
	logx.As().Debug().Str("chartRepo", effChartRepo.Get().Val()).
		Str("strategy", effChartRepo.Strategy().String()).
		Msg("Determined effective block node chart repo")

	effChartVersion, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ChartVersion() },
		resolver.RequiresExplicitOverride[string]("chartVersion", inputs.Custom.ChartVersion != "", force, hint),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart version: %v", err)
	}
	logx.As().Debug().Str("chartVersion", effChartVersion.Get().Val()).
		Str("strategy", effChartVersion.Strategy().String()).
		Msg("Determined effective block node chart version")

	effStorage, err := resolver.Field(
		func() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
			return h.base.Storage()
		},
		resolver.RequiresExplicitOverride[models.BlockNodeStorage](
			"storage", !inputs.Custom.Storage.IsEmpty(), force, hint,
		),
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node storage: %v", err)
	}
	logx.As().Debug().Any("storage", effStorage.Get().Val()).
		Str("strategy", effStorage.Strategy().String()).
		Msg("Determined effective block node storage")

	effective := models.UserInputs[models.BlocknodeInputs]{
		Common: inputs.Common,
		Custom: models.BlocknodeInputs{
			// Resolved via rsl
			Profile:      inputs.Custom.Profile,
			Release:      effReleaseName.Get().Val(),
			Version:      effVersion.Get().Val(),
			Namespace:    effNamespace.Get().Val(),
			ChartName:    effChartName.Get().Val(),
			Chart:        effChartRepo.Get().Val(),
			ChartVersion: effChartVersion.Get().Val(),
			Storage:      effStorage.Get().Val(),
			// Passed through from user input (no rsl resolution)
			ValuesFile:         inputs.Custom.ValuesFile,
			ReuseValues:        inputs.Custom.ReuseValues,
			SkipHardwareChecks: inputs.Custom.SkipHardwareChecks,
			ResetStorage:       inputs.Custom.ResetStorage,
		},
	}

	logx.As().Debug().Any("effectiveUserInputs", effective).
		Msg("Determined effective user inputs for block node install")

	return &effective, nil
}

// BuildWorkflow validates install preconditions and returns the workflow.
// If the cluster has already been created only the block node setup step is
// included; otherwise the full cluster bootstrap is prepended.
func (h *InstallHandler) BuildWorkflow(
	nodeState state.BlockNodeState,
	clusterState state.ClusterState,
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*automa.WorkflowBuilder, error) {
	if nodeState.ReleaseInfo.Status == release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is already installed; cannot install again").
			WithProperty(errPropertyResolution,
				"use 'weaver block node reset' or 'weaver block node upgrade', or pass --force to continue")
	}

	ins := inputs.Custom
	var wb *automa.WorkflowBuilder
	if clusterState.Created {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").
			Steps(setupBlockNode(ins))
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").
			Steps(
				installClusterWorkflow(models.NodeTypeBlock, ins.Profile, ins.SkipHardwareChecks),
				setupBlockNode(ins),
			)
	}
	return wb, nil
}
