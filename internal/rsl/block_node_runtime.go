package rsl

import (
	"time"

	"github.com/automa-saga/automa"
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
	*Base[state.BlockNodeState, models.BlocknodeInputs]
	intent *models.Intent
}

func (br *BlockNodeRuntimeResolver) Namespace() (*automa.EffectiveValue[string], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if br.state.ReleaseInfo.Namespace == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty namespace")
		}

		return automa.NewEffective[string](
			br.state.ReleaseInfo.Namespace,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil && br.inputs.Namespace != "" {
		return automa.NewEffective[string](
			br.inputs.Namespace,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.cfg.BlockNode.Namespace,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if err := br.state.Storage.Validate(); err != nil {
			return nil, errorx.IllegalState.
				Wrap(err, "block node runtime state is inconsistent")
		}

		return automa.NewEffective[models.BlockNodeStorage](
			br.state.Storage,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil {
		if err := br.state.Storage.Validate(); err == nil {
			return automa.NewEffective[models.BlockNodeStorage](
				br.inputs.Storage,
				automa.StrategyUserInput,
			)
		}
	}

	return automa.NewEffective[models.BlockNodeStorage](
		br.cfg.BlockNode.Storage,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) Version() (*automa.EffectiveValue[string], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if br.state.ReleaseInfo.Version == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty version")
		}

		return automa.NewEffective[string](
			br.state.ReleaseInfo.Version,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil && br.inputs.Version != "" {
		return automa.NewEffective[string](
			br.inputs.Version,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.cfg.BlockNode.Version,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) ReleaseName() (*automa.EffectiveValue[string], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if br.state.ReleaseInfo.Name == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty release name")
		}

		return automa.NewEffective[string](
			br.state.ReleaseInfo.Name,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil && br.inputs.Release != "" {
		return automa.NewEffective[string](
			br.inputs.Release,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.cfg.BlockNode.Release,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) ChartName() (*automa.EffectiveValue[string], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if br.state.ReleaseInfo.ChartName == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty chart name")
		}

		return automa.NewEffective[string](
			br.state.ReleaseInfo.ChartName,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil && br.inputs.ChartName != "" {
		return automa.NewEffective[string](
			br.inputs.ChartName,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.cfg.BlockNode.ChartName,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) ChartRef() (*automa.EffectiveValue[string], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if br.state.ReleaseInfo.ChartRef == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty chart ref")
		}

		return automa.NewEffective[string](
			br.state.ReleaseInfo.ChartRef,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil && br.inputs.Chart != "" {
		return automa.NewEffective[string](
			br.inputs.Chart,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.cfg.BlockNode.Chart,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) ChartVersion() (*automa.EffectiveValue[string], error) {
	if br.state != nil && br.state.ReleaseInfo.Status == release.StatusDeployed {
		if br.state.ReleaseInfo.ChartVersion == "" {
			return nil, errorx.IllegalState.
				New("block node runtime state is inconsistent: deployed release cannot have empty chart version")
		}

		return automa.NewEffective[string](
			br.state.ReleaseInfo.ChartVersion,
			automa.StrategyCurrent,
		)
	}

	if br.inputs != nil && br.inputs.ChartVersion != "" {
		return automa.NewEffective[string](
			br.inputs.ChartVersion,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.cfg.BlockNode.ChartVersion,
		automa.StrategyConfig,
	)
}

func (br *BlockNodeRuntimeResolver) WithUserInputs(inputs models.BlocknodeInputs) *BlockNodeRuntimeResolver {
	br.inputs = &inputs
	return br
}

func (br *BlockNodeRuntimeResolver) WithConfig(cfg models.Config) *BlockNodeRuntimeResolver {
	br.cfg = &cfg
	return br
}

func (br *BlockNodeRuntimeResolver) WithState(blockNodeState state.BlockNodeState) *BlockNodeRuntimeResolver {
	br.state = &blockNodeState
	return br
}

func NewBlockNodeRuntimeResolver(
	cfg models.Config,
	blockNodeState state.BlockNodeState,
	realityChecker reality.Checker[state.BlockNodeState],
	refreshInterval time.Duration,
) (*BlockNodeRuntimeResolver, error) {
	rb, err := NewRuntimeBase[state.BlockNodeState, models.BlocknodeInputs](
		cfg,
		blockNodeState,
		refreshInterval,
		realityChecker,
		// lastSyncFn extractor
		func(s *state.BlockNodeState) htime.Time { return s.LastSync },
		// cloneFn helper
		func(s *state.BlockNodeState) state.BlockNodeState { return s.Clone() },
		func() state.BlockNodeState { return state.BlockNodeState{} }, // default state
	)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create block-node runtime")
	}

	br := &BlockNodeRuntimeResolver{
		Base: rb,
	}

	return br, nil
}
