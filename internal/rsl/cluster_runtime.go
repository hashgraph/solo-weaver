package rsl

import (
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
) (*ClusterRuntimeResolver, error) {
	rb, err := NewRuntimeBase[state.ClusterState, models.ClusterInputs](
		cfg,
		clusterState,
		refreshInterval,
		realityChecker,
		func(s *state.ClusterState) htime.Time { return s.LastSync },
		func(s *state.ClusterState) (*state.ClusterState, error) { return s.Clone() },
		func() state.ClusterState { return state.ClusterState{} },
	)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create cluster runtime")
	}

	return &ClusterRuntimeResolver{
		Base: rb,
	}, nil
}
