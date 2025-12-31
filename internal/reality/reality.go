// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/core"
)

// Checker is the abstraction for accessing the current state of the system
// including cluster, machines, and blocknodes etc.
//
// It is used by the BLL to make decisions based on the actual state of the system.
// It is separate from the StateManager and BLL which manages the current state and executes intents.
//
// This separation allows for clearer distinction between current and actual states as well as intent execution.
// This is supposed to be resource heavy and may involve network calls, so it should be used judiciously.
type Checker interface {
	RefreshState(ctx context.Context, st *core.State) error

	ClusterState(ctx context.Context) (*core.ClusterState, error)

	MachineState(ctx context.Context) (*core.MachineState, error)

	BlockNodeState(ctx context.Context) (*core.BlockNodeState, error)
}

type realityChecker struct {
}

func (r *realityChecker) RefreshState(ctx context.Context, st *core.State) error {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) ClusterState(ctx context.Context) (*core.ClusterState, error) {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) MachineState(ctx context.Context) (*core.MachineState, error) {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) BlockNodeState(ctx context.Context) (*core.BlockNodeState, error) {
	//TODO implement me
	panic("implement me")
}

func NewChecker() Checker {
	return &realityChecker{}
}
