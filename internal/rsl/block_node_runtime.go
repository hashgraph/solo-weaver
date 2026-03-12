package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

// BlockNodeRuntimeResolver manages the current state of block-node related information,
// including configuration, user inputs, and reality-checked state.
//
// Resolution strategy (highest -> lowest precedence):
// 1. Current state (automa.StrategyCurrent)
//   - If `br.state != nil` and `br.state.ReleaseInfo.Status == release.StatusDeployed`,
//     the resolver returns the value from `br.state`.
//   - For string fields the deployed state is validated to be non-empty; an empty value
//     for a deployed release results in an `errorx.IllegalState` error.
//
// 2. User inputs (automa.StrategyUserInput)
//   - If no valid deployed current state is available and `br.inputs != nil` (and for
//     strings the input is non-empty), the resolver returns the user-provided value.
//
// 3. Config defaults (automa.StrategyConfig)
//   - If neither current state nor user inputs supply a value, the resolver returns
//     the config default value.
//
// Notes:
//   - Returned values are `*automa.EffectiveValue[T]` carrying the chosen value and the
//     strategy indicating its source.
//   - Structured types (e.g. `models.BlockNodeStorage`) follow the same precedence but do
//     not perform non-empty string validation.
//   - Helper methods `WithUserInputs`, `WithConfig`, and `WithState` set the resolver's
//     sources so resolution uses those values.
//   - The resolver relies on the runtime base for reality checking, last-sync extraction,
//     cloning and default state construction.
type BlockNodeRuntimeResolver struct {
	mu              sync.Mutex
	cfg             *models.Config
	state           *state.BlockNodeState
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.BlockNodeState]

	inputs *models.BlockNodeInputs
	intent *models.Intent
}

func (b *BlockNodeRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.intent = &intent
	return b
}

func (b *BlockNodeRuntimeResolver) WithUserInputs(inputs models.BlockNodeInputs) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.inputs = &inputs
	return b
}

func (b *BlockNodeRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.cfg = &cfg
	return b
}

func (b *BlockNodeRuntimeResolver) WithState(st state.BlockNodeState) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	b.state = &st
	b.mu.Unlock()

	return b
}

func (b *BlockNodeRuntimeResolver) Namespace() (*automa.EffectiveValue[string], error) {
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if b.state.ReleaseInfo.Namespace == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty namespace")
		}

		return automa.NewEffective[string](
			b.state.ReleaseInfo.Namespace,
			automa.StrategyCurrent,
		)
	}

	if b.inputs != nil && b.inputs.Namespace != "" {
		return automa.NewEffective[string](
			b.inputs.Namespace,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		b.cfg.BlockNode.Namespace,
		automa.StrategyConfig,
	)
}

func (b *BlockNodeRuntimeResolver) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if err := b.state.Storage.Validate(); err != nil {
			return nil, errorx.IllegalState.
				Wrap(err, "block node runtime state is inconsistent")
		}

		return automa.NewEffective[models.BlockNodeStorage](
			b.state.Storage,
			automa.StrategyCurrent,
		)
	}

	if b.inputs != nil {
		if err := b.state.Storage.Validate(); err == nil {
			return automa.NewEffective[models.BlockNodeStorage](
				b.inputs.Storage,
				automa.StrategyUserInput,
			)
		}
	}

	return automa.NewEffective[models.BlockNodeStorage](
		b.cfg.BlockNode.Storage,
		automa.StrategyConfig,
	)
}

func (b *BlockNodeRuntimeResolver) ReleaseName() (*automa.EffectiveValue[string], error) {
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if b.state.ReleaseInfo.Name == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty release name")
		}

		return automa.NewEffective[string](
			b.state.ReleaseInfo.Name,
			automa.StrategyCurrent,
		)
	}

	if b.inputs != nil && b.inputs.Release != "" {
		return automa.NewEffective[string](
			b.inputs.Release,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		b.cfg.BlockNode.Release,
		automa.StrategyConfig,
	)
}

func (b *BlockNodeRuntimeResolver) ChartName() (*automa.EffectiveValue[string], error) {
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if b.state.ReleaseInfo.ChartName == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty chart name")
		}

		return automa.NewEffective[string](
			b.state.ReleaseInfo.ChartName,
			automa.StrategyCurrent,
		)
	}

	if b.inputs != nil && b.inputs.ChartName != "" {
		return automa.NewEffective[string](
			b.inputs.ChartName,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		b.cfg.BlockNode.ChartName,
		automa.StrategyConfig,
	)
}

func (b *BlockNodeRuntimeResolver) ChartRef() (*automa.EffectiveValue[string], error) {
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if b.state.ReleaseInfo.ChartRef == "" {
			logx.As().Warn().Any("releaseInfo", b.state.ReleaseInfo).
				Msg("Block node runtime state is inconsistent: deployed release has empty chart ref; falling back to user input or config")
		}

		return automa.NewEffective[string](
			b.state.ReleaseInfo.ChartRef,
			automa.StrategyCurrent,
		)
	}

	if b.inputs != nil && b.inputs.Chart != "" {
		return automa.NewEffective[string](
			b.inputs.Chart,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		b.cfg.BlockNode.Chart,
		automa.StrategyConfig,
	)
}

// ChartVersion resolves the effective chart version for the block node based on the following precedence:
// 1. If the current state indicates a deployed release and the intent is an upgrade, the resolver checks for a
// user-provided chart version in the inputs. If provided, this takes precedence.
// 2. If no user input is provided for the chart version during an upgrade intent, but the current deployed state has
// a version, that version is used next.
// 3. If neither of the above provide a chart version during an upgrade intent, the resolver falls back to the config
// default.
// 4. For non-upgrade intents, if the current state indicates a deployed release, the chart version from the deployed
// state is used and cannot be overridden by user input or config.
// 5. If there is no deployed release, but user input provides a chart version, that is used next.
// 6. Finally, if none of the above sources provide a chart version, the resolver returns the config default.
//
// This resolution strategy ensures that during an upgrade, user input can specify a new chart version, but if not provided,
// the system retains the currently deployed version. For non-upgrade actions, the deployed version is authoritative if
// it exists, preventing unintended changes to the chart version.
func (b *BlockNodeRuntimeResolver) ChartVersion() (*automa.EffectiveValue[string], error) {
	// During an upgrade intent, the effective chart version is determined with the following precedence:
	// 1. User input: if the user provided a chart version in the inputs, that takes precedence.
	// 2. Current state version: if the current deployed state has a version, use that next.
	// 3. Config default: if neither of the above provide a version, fall back to the config default.
	if b.intent == nil {
		return nil, errorx.IllegalState.New("intent is not set in block node runtime resolver")
	}

	if b.intent.Action == models.ActionUpgrade {
		if b.state == nil || b.state.ReleaseInfo.Status != release.StatusDeployed {
			return nil, errorx.IllegalState.New(
				"block node is not deployed; cannot perform upgrade").
				WithProperty(models.ErrPropertyResolution,
					"upgrade action requires a currently deployed block node release")
		}

		if b.inputs.ChartVersion != "" {
			return automa.NewEffective[string](
				b.inputs.ChartVersion,
				automa.StrategyUserInput,
			)
		}

		if b.state.ReleaseInfo.Version != "" {
			return automa.NewEffective[string](
				b.state.ReleaseInfo.Version,
				automa.StrategyCurrent,
			)
		}

		return automa.NewEffective[string](
			b.cfg.BlockNode.ChartVersion,
			automa.StrategyConfig,
		)
	}

	// For non-upgrade intents, the chart version must come from the currently deployed release; user input and config cannot override it.
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if b.state.ReleaseInfo.ChartVersion == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty chart version")
		}

		return automa.NewEffective[string](
			b.state.ReleaseInfo.ChartVersion,
			automa.StrategyCurrent,
		)
	}

	// If there is no deployed release, but user input provides a chart version, that is used next.
	if b.inputs != nil && b.inputs.ChartVersion != "" {
		return automa.NewEffective[string](
			b.inputs.ChartVersion,
			automa.StrategyUserInput,
		)
	}

	// Finally, if none of the above sources provide a chart version, the resolver returns the config default.
	return automa.NewEffective[string](
		b.cfg.BlockNode.ChartVersion,
		automa.StrategyConfig,
	)
}

func (b *BlockNodeRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	logx.As().Debug().Msg("Refreshing block node state using reality checker")

	var err error
	var oldState *state.BlockNodeState
	if b.state != nil {
		oldState, err = b.state.Clone()
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to clone block node state for refresh")
		}
	}

	now := htime.Now()
	if !force {
		b.mu.Lock()
		if b.state != nil {
			if now.Sub(b.state.LastSync) < b.refreshInterval {
				b.mu.Unlock()
				logx.As().Debug().
					Time("lastSync", b.state.LastSync.Time).
					Dur("refreshInterval", b.refreshInterval).
					Dur("timeSinceLastSync", now.Sub(b.state.LastSync)).
					Msg("Block node state is fresh; skipping refresh")
				return nil
			}
		}
		b.mu.Unlock()
	}

	// Fetch the latest state directly from the reality checker
	st, err := b.realityChecker.RefreshState(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to refresh block node state")
	}

	// Replace the current state under lock.
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = &st
	b.state.LastSync = now

	// Preserve ChartRef across refresh if it was not set in reality but exists in the old state.
	if oldState != nil && b.state != nil {
		if b.state.ReleaseInfo.Status == release.StatusDeployed && b.state.ReleaseInfo.ChartRef == "" && oldState.ReleaseInfo.ChartRef != "" {
			logx.As().Debug().Msg("Preserving ChartRef from old state across refresh since reality checker did not provide it for deployed release")
			b.state.ReleaseInfo.ChartRef = oldState.ReleaseInfo.ChartRef
		}
	}

	logx.As().Debug().Any("state", b.state).Msg("Refreshed block node state using reality checker")

	return nil

}

func (b *BlockNodeRuntimeResolver) CurrentState() (state.BlockNodeState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == nil {
		return state.NewBlockNodeState(), errorx.IllegalState.New("cluster state is not initialized")
	}

	logx.As().Debug().Any("state", b.state).Msg("Reading current block node state")
	return *b.state, nil
}

func NewBlockNodeRuntimeResolver(
	cfg models.Config,
	blockNodeState state.BlockNodeState,
	realityChecker reality.Checker[state.BlockNodeState],
	refreshInterval time.Duration,
) (Resolver[state.BlockNodeState, models.BlockNodeInputs], error) {
	br := &BlockNodeRuntimeResolver{
		cfg:             &cfg,
		state:           &blockNodeState,
		realityChecker:  realityChecker,
		refreshInterval: refreshInterval,
	}

	return br, nil
}
