// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
)

// clusterChecker probes the Kubernetes cluster and returns a ClusterState.
// It depends only on a ClusterProbe (injectable) and kube.RetrieveClusterInfo.
type clusterChecker struct {
	sm            state.Manager
	clusterExists ClusterProbe
}

// NewClusterChecker constructs a clusterChecker with the given probe.
// In production pass kube.ClusterExists; in tests pass a fake.
func NewClusterChecker(sm state.Manager, clusterExists ClusterProbe) (Checker[state.ClusterState], error) {
	return &clusterChecker{sm: sm, clusterExists: clusterExists}, nil
}

func (c *clusterChecker) RefreshState(_ context.Context) (state.ClusterState, error) {
	cs := state.NewClusterState()

	exists, err := c.clusterExists()
	if !exists {
		logx.As().Debug().Err(err).Msg("Kubernetes cluster does not exist or is unreachable, returning empty ClusterState")
		return cs, nil
	}

	clusterInfo, err := kube.RetrieveClusterInfo()
	if err != nil {
		logx.As().Error().Err(err).Msg("Failed to retrieve cluster info, returning empty ClusterState")
		return cs, nil
	}

	cs.Initialize(clusterInfo)

	logx.As().Debug().Any("clusterState", cs).Msg("Refreshed cluster state")

	return cs, nil
}
