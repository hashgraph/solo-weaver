package rsl

import (
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

type ClusterRuntime struct {
	*Base[state.ClusterState]

	// keep reality if other cluster-specific methods need it
	reality reality.Checker
}

// NewClusterRuntime creates a ClusterRuntime with the provided configuration, initial state,
// reality checker, and refresh interval. The caller is responsible for retaining and injecting
// the returned instance — no package-level singleton is used.
func NewClusterRuntime(cfg models.Config, clusterState state.ClusterState, realityChecker reality.Checker, refreshInterval time.Duration) (*ClusterRuntime, error) {
	if realityChecker == nil {
		return nil, errorx.IllegalArgument.New("cluster reality checker is not initialized")
	}

	rb := NewRuntimeBase[state.ClusterState](
		cfg,
		clusterState,
		refreshInterval,
		realityChecker.ClusterState,
		func(s state.ClusterState) htime.Time { return s.LastSync },
		func(s state.ClusterState) state.ClusterState { return s.Clone() },
		func() state.ClusterState { return state.ClusterState{} },
		"cluster reality checker",
	)

	return &ClusterRuntime{
		Base:    rb,
		reality: realityChecker,
	}, nil
}
