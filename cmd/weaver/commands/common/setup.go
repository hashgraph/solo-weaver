// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

type SetupResult struct {
	Config         models.Config
	StateManager   state.Manager
	RealityChecker reality.Checkers
	Runtime        *rsl.RuntimeResolver
}

// Setup performs the common setup steps for all commands.
// It includes loading configuration, initializing the state manager, reality checker and runtime resolver
func Setup() (*SetupResult, error) {
	conf := config.Get()
	if err := conf.Validate(); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "invalid configuration")
	}

	logx.As().Debug().Any("config", conf).Msg("Loaded configuration")

	sm, err := state.NewStateManager()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create state manager")
	}

	if err = sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return nil, errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	currentState := sm.State()
	logx.As().Info().Str("state_file", currentState.StateFile).Any("currentState", currentState).Msg("Current state")

	realityChecker, err := reality.NewCheckers(sm)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create reality checker")
	}

	// Build the rsl.Runtime — single call that constructs cluster + block-node runtimes.
	runtime, err := rsl.NewRuntimeResolver(conf, sm, realityChecker, rsl.DefaultRefreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise rsl registry")
	}

	return &SetupResult{
		Config:         conf,
		StateManager:   sm,
		RealityChecker: realityChecker,
		Runtime:        runtime,
	}, nil
}
