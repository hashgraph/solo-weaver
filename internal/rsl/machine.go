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

type MachineRuntimeState struct {
	*Base[state.MachineState]

	// keep reality if other cluster-specific methods need it
	reality reality.Checker
}

// NewMachineRuntime creates a MachineRuntimeState with the provided configuration, initial state,
// reality checker, and refresh interval. The caller is responsible for retaining and injecting
// the returned instance — no package-level singleton is used.
func NewMachineRuntime(cfg models.Config, clusterState state.MachineState, realityChecker reality.Checker, refreshInterval time.Duration) (*MachineRuntimeState, error) {
	if realityChecker == nil {
		return nil, errorx.IllegalArgument.New("cluster reality checker is not initialized")
	}

	rb := NewRuntimeBase[state.MachineState](
		cfg,
		clusterState,
		refreshInterval,
		realityChecker.MachineState,
		func(s state.MachineState) htime.Time { return s.LastSync },
		func(s state.MachineState) state.MachineState { return s.Clone() },
		func() state.MachineState { return state.MachineState{} },
		"cluster reality checker",
	)

	return &MachineRuntimeState{
		Base:    rb,
		reality: realityChecker,
	}, nil
}
