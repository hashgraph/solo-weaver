package rsl

import (
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// BlockNodeRuntimeResolver manages the current state of block-node related information, including configuration, user inputs, and reality-checked state.
// It provides methods to access effective values for block-node properties, following
// the precedence of current state > user inputs > config defaults.
type BlockNodeRuntimeResolver struct {
	*Base[state.BlockNodeState, models.BlocknodeInputs]
	intent *models.Intent
}

func (br *BlockNodeRuntimeResolver) Namespace() (*automa.EffectiveValue[string], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[string](
				br.cfg.BlockNode.Namespace,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[string](
			br.inputs.Namespace,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.state.ReleaseInfo.Namespace,
		automa.StrategyCurrent,
	)
}

func (br *BlockNodeRuntimeResolver) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[models.BlockNodeStorage](
				br.cfg.BlockNode.Storage,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[models.BlockNodeStorage](
			br.inputs.Storage,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[models.BlockNodeStorage](
		br.state.Storage,
		automa.StrategyCurrent,
	)
}

func (br *BlockNodeRuntimeResolver) Version() (*automa.EffectiveValue[string], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[string](
				br.cfg.BlockNode.Version,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[string](
			br.inputs.Version,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.state.ReleaseInfo.Version,
		automa.StrategyCurrent,
	)
}

func (br *BlockNodeRuntimeResolver) ReleaseName() (*automa.EffectiveValue[string], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[string](
				br.cfg.BlockNode.Release,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[string](
			br.inputs.Release,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.state.ReleaseInfo.Name,
		automa.StrategyCurrent,
	)
}

func (br *BlockNodeRuntimeResolver) ChartName() (*automa.EffectiveValue[string], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[string](
				br.cfg.BlockNode.ChartName,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[string](
			br.inputs.ChartName,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.state.ReleaseInfo.ChartName,
		automa.StrategyCurrent,
	)
}

func (br *BlockNodeRuntimeResolver) ChartRepo() (*automa.EffectiveValue[string], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[string](
				br.cfg.BlockNode.Chart,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[string](
			br.inputs.Chart,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.state.ReleaseInfo.ChartRef,
		automa.StrategyCurrent,
	)
}

func (br *BlockNodeRuntimeResolver) ChartVersion() (*automa.EffectiveValue[string], error) {
	if br.state == nil {
		if br.inputs == nil {
			return automa.NewEffective[string](
				br.cfg.BlockNode.ChartVersion,
				automa.StrategyConfig,
			)
		}

		return automa.NewEffective[string](
			br.inputs.ChartVersion,
			automa.StrategyUserInput,
		)
	}

	return automa.NewEffective[string](
		br.state.ReleaseInfo.ChartVersion,
		automa.StrategyCurrent,
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
