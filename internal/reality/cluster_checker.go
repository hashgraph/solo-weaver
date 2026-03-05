// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/joomcode/errorx"
)

// clusterChecker probes the Kubernetes cluster and returns a ClusterState.
// It depends only on a ClusterProbe (injectable) and kube.RetrieveClusterInfo.
type clusterChecker struct {
	Base
	clusterExists ClusterProbe
}

// NewClusterChecker constructs a clusterChecker with the given probe.
// In production pass kube.ClusterExists; in tests pass a fake.
func NewClusterChecker(sm state.Manager, clusterExists ClusterProbe) Checker[state.ClusterState] {
	return &clusterChecker{
		Base:          Base{sm: sm},
		clusterExists: clusterExists}
}

func (c *clusterChecker) FlushState(st state.ClusterState) error {
	if err := c.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return ErrFlushError.Wrap(err, "failed to refresh state")
	}
	fullState := c.sm.State()
	fullState.ClusterState = st
	if err := c.sm.Set(fullState).Flush(); err != nil {
		return ErrFlushError.Wrap(err, "failed to persist state with refreshed ClusterState")
	}

	return nil
}

func (c *clusterChecker) RefreshState(ctx context.Context) (state.ClusterState, error) {
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

	// persist the refreshed state
	if err = c.FlushState(cs); err != nil {
		return cs, err
	}

	logx.As().Debug().Any("clusterState", cs).Msg("Refreshed cluster state")

	return cs, nil
}
