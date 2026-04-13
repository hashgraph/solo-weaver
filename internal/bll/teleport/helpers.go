// SPDX-License-Identifier: Apache-2.0

package teleport

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// resolveTeleportNodeEffectiveInputs resolves node agent fields via the RSL layer.
func resolveTeleportNodeEffectiveInputs(
	runtime *rsl.TeleportRuntimeResolver,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportNodeInputs],
) (*models.UserInputs[models.TeleportNodeInputs], error) {
	runtime.WithIntent(intent).WithUserInputs(inputs.Custom)

	effToken, err := runtime.Token()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve teleport node agent token: %v", err)
	}
	logx.As().Debug().
		Bool("tokenResolved", effToken.Get().Val() != "").
		Msg("Determined effective teleport node agent token")

	effProxyAddr, err := runtime.ProxyAddr()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve teleport node agent proxy address: %v", err)
	}
	logx.As().Debug().
		Any("proxyAddr", effProxyAddr).
		Msg("Determined effective teleport node agent proxy address")

	effectiveInputs := models.UserInputs[models.TeleportNodeInputs]{
		Common: inputs.Common,
		Custom: models.TeleportNodeInputs{
			Token:     effToken.Get().Val(),
			ProxyAddr: effProxyAddr.Get().Val(),
		},
	}

	logx.As().Debug().Any("effectiveUserInputs", effectiveInputs.Custom.Redacted()).
		Msg("Determined effective user inputs for teleport node agent")

	return &effectiveInputs, nil
}

// resolveTeleportClusterEffectiveInputs resolves cluster agent fields via the RSL layer.
func resolveTeleportClusterEffectiveInputs(
	runtime *rsl.TeleportRuntimeResolver,
	intent models.Intent,
	inputs models.UserInputs[models.TeleportClusterInputs],
) (*models.UserInputs[models.TeleportClusterInputs], error) {
	runtime.WithIntent(intent)
	runtime.WithClusterInputs(inputs.Custom)

	effVersion, err := runtime.Version()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve teleport cluster agent version: %v", err)
	}
	logx.As().Debug().
		Any("version", effVersion).
		Msg("Determined effective teleport cluster agent version")

	effValuesFile, err := runtime.ValuesFile()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve teleport cluster agent values file: %v", err)
	}
	logx.As().Debug().
		Any("valuesFile", effValuesFile).
		Msg("Determined effective teleport cluster agent values file")

	effNamespace, err := runtime.Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve teleport cluster agent namespace: %v", err)
	}
	logx.As().Debug().
		Any("namespace", effNamespace).
		Msg("Determined effective teleport cluster agent namespace")

	effReleaseName, err := runtime.ReleaseName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve teleport cluster agent release name: %v", err)
	}
	logx.As().Debug().
		Any("releaseName", effReleaseName).
		Msg("Determined effective teleport cluster agent release name")

	effectiveInputs := models.UserInputs[models.TeleportClusterInputs]{
		Common: inputs.Common,
		Custom: models.TeleportClusterInputs{
			Version:    effVersion.Get().Val(),
			ValuesFile: effValuesFile.Get().Val(),
		},
	}

	logx.As().Debug().Any("effectiveUserInputs", effectiveInputs).
		Msg("Determined effective user inputs for teleport cluster agent")

	return &effectiveInputs, nil
}
