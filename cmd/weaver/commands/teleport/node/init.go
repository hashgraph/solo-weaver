// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	blltp "github.com/hashgraph/solo-weaver/internal/bll/teleport"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/joomcode/errorx"
)

var teleportHandler *blltp.HandlerRegistry

func initializeDependencies() error {
	conf := config.Get()
	if err := conf.Validate(); err != nil {
		return errorx.IllegalState.Wrap(err, "invalid configuration")
	}

	logx.As().Debug().Msg("Loaded configuration")

	sm, err := state.NewStateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager")
	}

	if err = sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	realityChecker, err := reality.NewCheckers(sm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create reality checker")
	}

	runtime, err := rsl.NewRuntimeResolver(conf, sm, realityChecker, rsl.DefaultRefreshInterval)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to initialise rsl registry")
	}

	defaults := config.DefaultsConfig()
	envVals := config.EnvConfig()
	logx.As().Debug().Any("defaults_config", defaults).Msg("Setting teleport defaults")
	runtime.TeleportRuntime.WithDefaults(defaults)
	logx.As().Debug().Any("env_config", envVals).Msg("Setting teleport env config")
	runtime.TeleportRuntime.WithEnv(envVals)

	teleportHandler, err = blltp.NewHandlerFactory(sm, runtime)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to initialise teleport intent handler")
	}

	return nil
}
