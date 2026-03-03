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
	clusterExists ClusterProbe
}

// Ensure clusterChecker satisfies ClusterChecker at compile time.
var _ ClusterChecker = (*clusterChecker)(nil)

// newClusterChecker constructs a clusterChecker with the given probe.
// In production pass kube.ClusterExists; in tests pass a fake.
func newClusterChecker(clusterExists ClusterProbe) ClusterChecker {
	return &clusterChecker{clusterExists: clusterExists}
}

// ClusterState probes the Kubernetes cluster and returns a ClusterState.
// If the cluster does not exist or is unreachable, it returns an empty ClusterState.
func (c *clusterChecker) ClusterState(ctx context.Context) (state.ClusterState, error) {
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
	return cs, nil
}
