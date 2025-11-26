package core

import (
	"context"
)

// RealityChecker is the abtraction for accessing the current state of the system
// including cluster, machines, and blocknodes etc.
//
// It is used by the BLL to make decisions based on the actual state of the system.
// It is separate from the StateManager and BLL which manages the current state and executes intents.
//
// This separation allows for clearer distinction between current and actual states as well as intent execution.
// This is supposed to be resource heavy and may involve network calls, so it should be used judiciously.
type RealityChecker interface {
	RefreshState(ctx context.Context, st *State) error

	ClusterState(ctx context.Context) (*ClusterState, error)

	MachineState(ctx context.Context) (*MachineState, error)

	BlocknodeState(ctx context.Context) (*BlockNodeState, error)
}

type realityChecker struct {
}

func (r *realityChecker) RefreshState(ctx context.Context, st *State) error {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) ClusterState(ctx context.Context) (*ClusterState, error) {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) MachineState(ctx context.Context) (*MachineState, error) {
	//TODO implement me
	panic("implement me")
}

func (r *realityChecker) BlocknodeState(ctx context.Context) (*BlockNodeState, error) {
	//TODO implement me
	panic("implement me")
}

func NewRealityChecker() RealityChecker {
	return &realityChecker{}
}
