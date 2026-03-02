// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/Masterminds/semver/v3"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/resolver"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// UpgradeHandler handles the ActionUpgrade intent for a block node.
// Unlike InstallHandler, it does NOT apply RequiresExplicitOverride guards —
// the whole point of an upgrade is to change version/chart fields.  Instead it
// enforces chart immutability (you cannot switch charts during an upgrade) and
// prevents version downgrade.
type UpgradeHandler struct {
	base rslAccessor
}

func newUpgradeHandler(base rslAccessor) *UpgradeHandler {
	return &UpgradeHandler{base: base}
}

// PrepareEffectiveInputs resolves fields for an upgrade.  Chart immutability
// and semver constraints are enforced inside BuildWorkflow so that all
// precondition errors are reported together after resolution succeeds.
//
// Unlike InstallHandler, no RequiresExplicitOverride validators are registered
// here — the operator explicitly intends to change these fields.  The zero-
// validator resolver.Field calls are intentional: they make the absence of
// guards visible at the call site and allow validators to be added later
// without changing the call pattern.
func (h *UpgradeHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*models.UserInputs[models.BlocknodeInputs], error) {
	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}

	// No validators — upgrade is explicitly permitted to change any field.
	effReleaseName, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ReleaseName() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node release name: %v", err)
	}
	logx.As().Debug().Str("releaseName", effReleaseName.Get().Val()).
		Str("strategy", effReleaseName.Strategy().String()).
		Msg("Determined effective block node release name")

	effVersion, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.Version() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node version: %v", err)
	}
	logx.As().Debug().Str("version", effVersion.Get().Val()).
		Str("strategy", effVersion.Strategy().String()).
		Msg("Determined effective block node version")

	effNamespace, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.Namespace() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node namespace: %v", err)
	}
	logx.As().Debug().Str("namespace", effNamespace.Get().Val()).
		Str("strategy", effNamespace.Strategy().String()).
		Msg("Determined effective block node namespace")

	effChartName, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ChartName() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart name: %v", err)
	}
	logx.As().Debug().Str("chartName", effChartName.Get().Val()).
		Str("strategy", effChartName.Strategy().String()).
		Msg("Determined effective block node chart name")

	effChartRepo, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ChartRepo() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart repo: %v", err)
	}
	logx.As().Debug().Str("chartRepo", effChartRepo.Get().Val()).
		Str("strategy", effChartRepo.Strategy().String()).
		Msg("Determined effective block node chart repo")

	effChartVersion, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) { return h.base.ChartVersion() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart version: %v", err)
	}
	logx.As().Debug().Str("chartVersion", effChartVersion.Get().Val()).
		Str("strategy", effChartVersion.Strategy().String()).
		Msg("Determined effective block node chart version")

	effStorage, err := resolver.Field(
		func() (*automa.EffectiveValue[models.BlockNodeStorage], error) { return h.base.Storage() },
	)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node storage: %v", err)
	}

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
		Msg("Determined effective user inputs for block node upgrade")

	return &effective, nil
}

// BuildWorkflow validates upgrade preconditions and returns the workflow.
// Preconditions:
//   - Block node must already be deployed.
//   - Chart repository cannot be changed during an upgrade.
//   - Version cannot be downgraded.
func (h *UpgradeHandler) BuildWorkflow(
	nodeState state.BlockNodeState,
	_ state.ClusterState,
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*automa.WorkflowBuilder, error) {
	if nodeState.ReleaseInfo.Status != release.StatusDeployed {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot upgrade").
			WithProperty(errPropertyResolution, "use 'weaver block node install' to install the block node")
	}

	if nodeState.ReleaseInfo.ChartRef != inputs.Custom.Chart {
		return nil, errorx.IllegalState.New(
			"block node chart is already set to %q; chart cannot be changed during an upgrade",
			nodeState.ReleaseInfo.ChartRef).
			WithProperty(errPropertyResolution, "use 'weaver block node reset' then 'weaver block node install'")
	}

	currentVer, err := semver.NewVersion(nodeState.ReleaseInfo.Version)
	if err != nil {
		return nil, errorx.IllegalState.New(
			"failed to parse current block node version %q: %v", nodeState.ReleaseInfo.Version, err)
	}
	desiredVer, err := semver.NewVersion(inputs.Custom.Version)
	if err != nil {
		return nil, errorx.IllegalState.New(
			"failed to parse desired block node version %q: %v", inputs.Custom.Version, err)
	}
	if desiredVer.LessThan(currentVer) {
		return nil, errorx.IllegalArgument.New(
			"block node version cannot be downgraded from %q to %q",
			currentVer, desiredVer)
	}
	if desiredVer.Equal(currentVer) && !inputs.Common.Force {
		return nil, errorx.IllegalArgument.New(
			"block node is already at version %q; use --force to re-apply", currentVer)
	}

	ins := inputs.Custom
	var wb *automa.WorkflowBuilder
	if ins.ResetStorage {
		wb = automa.NewWorkflowBuilder().WithId("block-node-upgrade-with-reset").
			Steps(purgeBlockNodeStorage(ins), upgradeBlockNode(ins))
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-upgrade").
			Steps(upgradeBlockNode(ins))
	}
	return wb, nil
}
