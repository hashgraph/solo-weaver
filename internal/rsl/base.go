package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

const DefaultRefreshInterval = 10 * time.Minute
const DefaultRefreshTimeout = 60 * time.Second

// Base centralizes refresh / current-state behavior for any state type T.
// T must not be a pointer type.
type Base[T any] struct {
	mu              sync.Mutex
	cfg             models.Config
	current         T
	refreshInterval time.Duration

	// fetch obtains the latest state; required.
	fetch     func(context.Context) (T, error)
	fetchName string

	// optional helpers
	lastSync     func(T) htime.Time
	clone        func(T) T
	defaultState func() T
}

// NewRuntimeBase constructs a runtimeBase for a given initial value and helpers.
func NewRuntimeBase[T any](
	cfg models.Config,
	initial T,
	refreshInterval time.Duration,
	fetch func(context.Context) (T, error),
	lastSync func(T) htime.Time,
	clone func(T) T,
	defaultState func() T,
	fetchName string,
) *Base[T] {
	return &Base[T]{
		current:         initial,
		cfg:             cfg,
		refreshInterval: refreshInterval,
		fetch:           fetch,
		lastSync:        lastSync,
		clone:           clone,
		defaultState:    defaultState,
		fetchName:       fetchName,
	}
}

// RefreshState refreshes the current state using the configured fetch function.
// It respects refreshInterval if lastSync is provided.
func (rb *Base[T]) RefreshState(ctx context.Context, force bool) error {
	if rb.fetch == nil {
		return errorx.IllegalState.New("%s fetcher is not initialized", rb.fetchName)
	}

	now := htime.Now()
	// check last sync freshness if helper provided
	if !force && rb.lastSync != nil {
		rb.mu.Lock()
		ls := rb.lastSync(rb.current)
		rb.mu.Unlock()
		if now.Sub(ls) < rb.refreshInterval {
			return nil
		}
	}

	// fetch and replace under lock
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// re-check fetch presence under lock (defensive)
	if rb.fetch == nil {
		return nil
	}

	st, err := rb.fetch(ctx)
	if err != nil {
		return err
	}

	rb.current = st
	return nil
}

// CurrentState returns a copy/clone of the current state.
func (rb *Base[T]) CurrentState() (T, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.clone != nil {
		return rb.clone(rb.current), nil
	}

	return rb.defaultState(), errorx.IllegalState.New("clone function is not defined for current state")
}
