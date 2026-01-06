package runtime

import (
	"time"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

var clusterRuntimeSingleton *ClusterRuntime

type ClusterRuntime struct {
	*Base[core.ClusterState]

	// keep reality if other cluster-specific methods need it
	reality reality.Checker
}

func InitClusterRuntime(cfg config.Config, state core.ClusterState, realityChecker reality.Checker, refreshInterval time.Duration) error {
	if realityChecker == nil {
		return errorx.IllegalArgument.New("cluster reality checker is not initialized")
	}

	rb := NewRuntimeBase[core.ClusterState](
		cfg,
		state,
		refreshInterval,
		// fetch function
		realityChecker.ClusterState,
		// lastSync extractor
		func(s *core.ClusterState) htime.Time { return s.LastSync },
		// clone helper
		func(s *core.ClusterState) *core.ClusterState { return s.Clone() },
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
