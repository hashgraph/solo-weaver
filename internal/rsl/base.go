package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
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

	// optional helpers
	lastSyncFn func(*S) htime.Time
	cloneFn    func(*S) (*S, error)
	defaultFn  func() S

	inputs *I
}

// NewRuntimeBase constructs a runtimeBase for a given initial value and helpers.
func NewRuntimeBase[S any, I any](
	cfg models.Config,
	state S,
	refreshInterval time.Duration,
	realityChecker reality.Checker[S],
	lastSyncFn func(*S) htime.Time,
	cloneFn func(*S) (*S, error),
	defaultStateFn func() S,
) (*Base[S, I], error) {
	if realityChecker == nil {
		return nil, errorx.IllegalArgument.New("realityChecker function is required for Base")
	}

	return &Base[S, I]{
		state:           &state,
		cfg:             &cfg,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
		lastSyncFn:      lastSyncFn,
		cloneFn:         cloneFn,
		defaultFn:       defaultStateFn,
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

// RefreshState refreshes the current state using the configured fetchFn function.
// It respects refreshInterval if lastSyncFn is provided.
func (b *Base[S, I]) RefreshState(ctx context.Context, force bool) error {
	now := htime.Now()
	// check last sync freshness if helper provided
	if !force && b.lastSyncFn != nil {
		b.mu.Lock()
		ls := b.lastSyncFn(b.state)
		b.mu.Unlock()
		if now.Sub(ls) < b.refreshInterval {
			return nil
		}
	}

	// fetchFn and replace under lock
	b.mu.Lock()
	defer b.mu.Unlock()

	st, err := b.realityChecker.RefreshState(ctx)
	if err != nil {
		return err
	}

	b.state = &st
	return nil
}

// CurrentState returns a copy/cloneFn of the current state.
func (b *Base[S, I]) CurrentState() (S, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cloneFn != nil {
		c, err := b.cloneFn(b.state)
		if err != nil {
			return b.defaultFn(), errorx.IllegalState.Wrap(err, "cloneFn failed for current state")
		}
		return *c, nil
	}

	return b.defaultFn(), errorx.IllegalState.New("cloneFn function is not defined for current state")
}
