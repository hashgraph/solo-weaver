package rsl

import (
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

const DefaultRefreshInterval = 10 * time.Minute
const DefaultRefreshTimeout = 60 * time.Second

// Base centralizes refresh / current-state behavior for any state type T.
// T must not be a pointer type.
type Base[S any, I any] struct {
	mu              sync.Mutex
	cfg             *models.Config
	state           *S
	refreshInterval time.Duration
	realityChecker  reality.Checker[S]

	inputs *I
}

// NewRuntimeBase constructs a runtimeBase for a given initial value and helpers.
func NewRuntimeBase[S any, I any](
	cfg models.Config,
	state S,
	refreshInterval time.Duration,
	realityChecker reality.Checker[S],
) (*Base[S, I], error) {
	if realityChecker == nil {
		return nil, errorx.IllegalArgument.New("realityChecker function is required for Base")
	}

	return &Base[S, I]{
		state:           &state,
		cfg:             &cfg,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
	}, nil
}

func (b *Base[S, I]) WithUserInputs(inputs I) *Base[S, I] {
	b.inputs = &inputs
	return b
}

func (b *Base[S, I]) WithConfig(cfg models.Config) *Base[S, I] {
	b.cfg = &cfg
	return b
}

func (b *Base[S, I]) WithState(blockNodeState S) *Base[S, I] {
	b.state = &blockNodeState
	return b
}
