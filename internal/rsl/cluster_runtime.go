package rsl

import (
	"context"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

type ClusterRuntimeResolver struct {
	*Base[state.ClusterState, models.ClusterInputs]
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
	rb, err := NewRuntimeBase[state.ClusterState, models.ClusterInputs](
		cfg,
		clusterState,
		refreshInterval,
		realityChecker,
	)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create cluster runtime")
	}

	return &ClusterRuntimeResolver{
		Base: rb,
	}, nil
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
