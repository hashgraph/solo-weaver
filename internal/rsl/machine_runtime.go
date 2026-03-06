// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

type MachineRuntimeResolver struct {
	*Base[state.MachineState, models.MachineInputs]
}

// NewMachineRuntimeResolver creates a MachineRuntimeResolver with the provided configuration, initial state,
// reality checker, and refresh interval. The caller is responsible for retaining and injecting
// the returned instance — no package-level singleton is used.
func NewMachineRuntimeResolver(
	cfg models.Config,
	clusterState state.MachineState,
	realityChecker reality.Checker[state.MachineState],
	refreshInterval time.Duration,
) (*MachineRuntimeResolver, error) {
	rb, err := NewRuntimeBase[state.MachineState, models.MachineInputs](
		cfg,
		clusterState,
		refreshInterval,
		realityChecker,
		func(s *state.MachineState) htime.Time { return s.LastSync },
		func(s *state.MachineState) (*state.MachineState, error) { return s.Clone() },
		func() state.MachineState { return state.MachineState{} },
	)

	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create machine runtime")
	}

	return &MachineRuntimeResolver{
		Base: rb,
	}, nil
}
