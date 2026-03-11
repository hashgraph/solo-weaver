package rsl

import (
	"context"
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
	*Base[state.BlockNodeState, models.BlockNodeInputs]
	intent *models.Intent
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

func (b *BlockNodeRuntimeResolver) Version() (*automa.EffectiveValue[string], error) {
	if b.state != nil && b.state.ReleaseInfo.Status == release.StatusDeployed {
		if b.state.ReleaseInfo.Version == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty version")
		}

		return automa.NewEffective[string](
			b.state.ReleaseInfo.Version,
			automa.StrategyCurrent,
		)
	}

	if b.inputs != nil && b.inputs.Version != "" {
		return automa.NewEffective[string](
			b.inputs.Version,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		b.cfg.BlockNode.Version,
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

func (b *BlockNodeRuntimeResolver) ChartVersion() (*automa.EffectiveValue[string], error) {
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

	if b.inputs != nil && b.inputs.ChartVersion != "" {
		return automa.NewEffective[string](
			b.inputs.ChartVersion,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		b.cfg.BlockNode.ChartVersion,
		automa.StrategyConfig,
	)
}

func (b *BlockNodeRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
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

	// Preserve ChartRef across refresh if it was not set in reality but exists in the old state.
	if oldState != nil && b.state != nil {
		if b.state.ReleaseInfo.Status == release.StatusDeployed && b.state.ReleaseInfo.ChartRef == "" && oldState.ReleaseInfo.ChartRef != "" {
			b.state.ReleaseInfo.ChartRef = oldState.ReleaseInfo.ChartRef
		}
	}

	return nil

}

func (b *BlockNodeRuntimeResolver) CurrentState() (state.BlockNodeState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == nil {
		return state.NewBlockNodeState(), errorx.IllegalState.New("cluster state is not initialized")
	}

	return *b.state, nil
}

func NewBlockNodeRuntimeResolver(
	cfg models.Config,
	blockNodeState state.BlockNodeState,
	realityChecker reality.Checker[state.BlockNodeState],
	refreshInterval time.Duration,
) (Resolver[state.BlockNodeState, models.BlockNodeInputs], error) {
	rb, err := NewRuntimeBase[state.BlockNodeState, models.BlockNodeInputs](
		cfg,
		blockNodeState,
		refreshInterval,
		realityChecker,
	)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create block-node runtime")
	}

	br := &BlockNodeRuntimeResolver{
		Base: rb,
	}

	return br, nil
}
