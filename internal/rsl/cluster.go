package rsl

import (
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

var clusterRuntimeSingleton *ClusterRuntime

type ClusterRuntime struct {
	*Base[state.ClusterState]

	// keep reality if other cluster-specific methods need it
	reality reality.Checker
}

func InitClusterRuntime(cfg models.Config, clusterState state.ClusterState, realityChecker reality.Checker, refreshInterval time.Duration) error {
	if realityChecker == nil {
		return errorx.IllegalArgument.New("cluster reality checker is not initialized")
	}

	rb := NewRuntimeBase[state.ClusterState](
		cfg,
		clusterState,
		refreshInterval,
		// fetch function
		realityChecker.ClusterState,
		// lastSync extractor
		func(s state.ClusterState) htime.Time { return s.LastSync },
		// clone helper
		func(s state.ClusterState) state.ClusterState { return s.Clone() },
		func() state.ClusterState { return state.ClusterState{} },
		"cluster reality checker",
	)

	clusterRuntimeSingleton = &ClusterRuntime{
		Base:    rb,
		reality: realityChecker,
	}

	return nil
}

func Cluster() *ClusterRuntime {
	return clusterRuntimeSingleton
}
