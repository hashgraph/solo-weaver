// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/logx"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// patchBlockNodeState persists fields that cannot be recovered from the Helm
// release or PersistentVolumes into the runtime state before flushing to disk.
//
//   - ChartRef: the OCI / repo reference is not stored in Helm release metadata.
//   - Storage.BasePath: a weaver concept; Kubernetes only knows the individual PV
//     hostPaths that the reality checker reads back. Without this patch, BasePath
//     is lost after every FlushState → Refresh cycle, and the next interactive
//     prompt would re-fill the PV-derived individual paths instead of the
//     operator-chosen base path.
//
// Profile persistence is handled centrally by BaseHandler.FlushState via ProfileExtractor.
func patchBlockNodeState() func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
	return func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
		if st.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed {
			return nil
		}
		if effectiveInputs.Custom.Chart != "" {
			logx.As().Debug().Str("chartRef", effectiveInputs.Custom.Chart).
				Msg("Persisted block node chart ref into runtime state")
			st.BlockNodeState.ReleaseInfo.ChartRef = effectiveInputs.Custom.Chart
		}
		if effectiveInputs.Custom.Storage.BasePath != "" {
			logx.As().Debug().Str("basePath", effectiveInputs.Custom.Storage.BasePath).
				Msg("Persisted block node storage base path into runtime state")
			st.BlockNodeState.Storage.BasePath = effectiveInputs.Custom.Storage.BasePath
		}
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

	effHistoricRetention, err := runtime.HistoricRetention()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node historic retention: %v", err)
	}
	logx.As().Debug().
		Any("historicRetention", effHistoricRetention).
		Msg("Determined effective block node historic retention")

	effRecentRetention, err := runtime.RecentRetention()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node recent retention: %v", err)
	}
	logx.As().Debug().
		Any("recentRetention", effRecentRetention).
		Msg("Determined effective block node recent retention")

	effectiveInputs := models.UserInputs[models.BlockNodeInputs]{
		Common: inputs.Common,
		Custom: models.BlockNodeInputs{
			// Resolved via resolver
			Profile:           inputs.Custom.Profile,
			Release:           effReleaseName.Get().Val(),
			Namespace:         effNamespace.Get().Val(),
			ChartName:         effChartName.Get().Val(),
			Chart:             effChartRepo.Get().Val(),
			ChartVersion:      effChartVersion.Get().Val(),
			Storage:           effStorage.Get().Val(),
			HistoricRetention: effHistoricRetention.Get().Val(),
			RecentRetention:   effRecentRetention.Get().Val(),
			// Passed through from user input (no resolution)
			ValuesFile:          inputs.Custom.ValuesFile,
			ReuseValues:         inputs.Custom.ReuseValues,
			SkipHardwareChecks:  inputs.Custom.SkipHardwareChecks,
			ResetStorage:        inputs.Custom.ResetStorage,
			LoadBalancerEnabled: inputs.Custom.LoadBalancerEnabled,
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

// storagePathsChanged returns true when the requested storage configuration differs
// from the currently deployed one. Both sides are resolved through a Manager so that
// base-path expansion and sanitization are applied consistently before comparing.
func storagePathsChanged(deployed models.BlockNodeStorage, requested models.BlockNodeInputs) (bool, error) {
	deployedInputs := requested
	deployedInputs.Storage = deployed

	deployedMgr, err := bnpkg.NewManager(deployedInputs)
	if err != nil {
		return false, err
	}
	dArchive, dLive, dLog, dOpt, err := deployedMgr.GetStoragePaths()
	if err != nil {
		return false, err
	}

	requestedMgr, err := bnpkg.NewManager(requested)
	if err != nil {
		return false, err
	}
	rArchive, rLive, rLog, rOpt, err := requestedMgr.GetStoragePaths()
	if err != nil {
		return false, err
	}

	if dArchive != rArchive || dLive != rLive || dLog != rLog {
		return true, nil
	}
	for i := range rOpt {
		if i >= len(dOpt) || dOpt[i] != rOpt[i] {
			return true, nil
		}
	}
	return false, nil
}
