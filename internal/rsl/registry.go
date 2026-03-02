// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Registry holds the fully-initialised runtime components for all node types.
// It is constructed once at the composition root (main / command init) and injected
// into every layer that needs it, eliminating package-level singletons.
type Registry struct {
	BlockNode *BlockNodeRuntimeState
	Cluster   *ClusterRuntime
}

// NewRegistry creates a Registry by constructing each runtime from the supplied
// configuration, current persisted state, reality checker, and refresh interval.
func NewRegistry(
	cfg models.Config,
	currentState state.State,
	realityChecker reality.Checker,
	refreshInterval time.Duration,
) (*Registry, error) {
	if realityChecker == nil {
		return nil, errorx.IllegalArgument.New("reality checker cannot be nil")
	}

	clusterRuntime, err := NewClusterRuntime(cfg, currentState.ClusterState, realityChecker, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise cluster runtime")
	}

	blockNodeRuntime, err := NewBlockNodeRuntime(cfg, currentState.BlockNodeState, realityChecker, refreshInterval)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to initialise block-node runtime")
	}

	return &Registry{
		Cluster:   clusterRuntime,
		BlockNode: blockNodeRuntime,
	}, nil
}
