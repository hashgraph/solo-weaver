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
func NewClusterChecker(sm state.Manager, clusterExists ClusterProbe) (Checker[state.ClusterState], error) {
	return &clusterChecker{
		Base:          Base{sm: sm},
		clusterExists: clusterExists}, nil
}

// FlushState first refreshes the state from disk to get the latest ClusterState,
// then compares the existing ClusterState with the new one. If they are equal,
// no write is performed. If they differ, the new ClusterState is persisted to disk.
// This pattern of refreshing before writing is necessary to prevent overwriting
// concurrent updates to other parts of the state by separate reality checkers.
func (c *clusterChecker) FlushState(st state.ClusterState) error {
	if err := c.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return ErrFlushError.Wrap(err, "failed to refresh state")
	}

	existing := c.sm.State().ClusterState
	if existing.Equal(st) {
		return nil
	}

	fullState := c.sm.State()
	fullState.ClusterState = st
	if err := c.sm.Set(fullState).FlushState(); err != nil {
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
