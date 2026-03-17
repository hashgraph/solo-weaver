package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

type ClusterRuntimeResolver struct {
	mu              sync.Mutex
	cfg             *models.Config
	state           *state.ClusterState
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.ClusterState]

	intent *models.Intent
	inputs *models.ClusterInputs
}

func (c *ClusterRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.ClusterState, models.ClusterInputs] {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.intent = &intent
	return c
}

func (c *ClusterRuntimeResolver) WithUserInputs(inputs models.ClusterInputs) Resolver[state.ClusterState, models.ClusterInputs] {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.inputs = &inputs
	return c
}

func (c *ClusterRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.ClusterState, models.ClusterInputs] {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cfg = &cfg
	return c
}

func (c *ClusterRuntimeResolver) WithState(st state.ClusterState) Resolver[state.ClusterState, models.ClusterInputs] {
	c.mu.Lock()
	c.state = &st
	c.mu.Unlock()

	return c
}

// RefreshState refreshes the current state using the configured reality checker.
// It respects refreshInterval by comparing against the concrete ClusterState.LastSync
// field rather than using an injected helper.
func (c *ClusterRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	now := htime.Now()

	// If not forcing a refresh, check the last sync time directly from the concrete state.
	if !force {
		c.mu.Lock()
		// If state is nil or the last sync is older than the refresh interval, proceed.
		if c.state != nil {
			if now.Sub(c.state.LastSync) < c.refreshInterval {
				c.mu.Unlock()
				return nil
			}
		}
		c.mu.Unlock()
	}

	// Fetch the latest state from the reality checker.
	st, err := c.realityChecker.RefreshState(ctx)
	if err != nil {
		return err
	}

	// Replace the current state under lock.
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = &st
	return nil
}

func (c *ClusterRuntimeResolver) CurrentState() (state.ClusterState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == nil {
		return state.NewClusterState(), errorx.IllegalState.New("cluster state is not initialized")
	}

	return *c.state, nil
}

// NewClusterRuntimeResolver creates a ClusterRuntime with the provided configuration, initial state,
// reality checker, and refresh interval. The caller is responsible for retaining and injecting
// the returned instance — no package-level singleton is used.
func NewClusterRuntimeResolver(
	cfg models.Config,
	clusterState state.ClusterState,
	realityChecker reality.Checker[state.ClusterState],
	refreshInterval time.Duration,
) (Resolver[state.ClusterState, models.ClusterInputs], error) {
	return &ClusterRuntimeResolver{
		cfg:             &cfg,
		state:           &clusterState,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
	}, nil
}
