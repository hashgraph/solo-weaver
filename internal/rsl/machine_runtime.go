// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
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
) (Resolver[state.MachineState, models.MachineInputs], error) {
	rb, err := NewRuntimeBase[state.MachineState, models.MachineInputs](
		cfg,
		clusterState,
		refreshInterval,
		realityChecker,
	)

	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create machine runtime")
	}

	return &MachineRuntimeResolver{
		Base: rb,
	}, nil
}

// RefreshState refreshes the current machine state from the reality checker, using the
// concrete MachineState.LastSync to apply refresh interval checks.
func (m *MachineRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	now := htime.Now()

	if !force {
		m.mu.Lock()
		if m.state != nil {
			if now.Sub(m.state.LastSync) < m.refreshInterval {
				m.mu.Unlock()
				return nil
			}
		}
		m.mu.Unlock()
	}

	st, err := m.realityChecker.RefreshState(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = &st
	return nil
}

func (m *MachineRuntimeResolver) CurrentState() (state.MachineState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		return state.NewMachineState(), errorx.IllegalState.New("machine state is not initialized")
	}

	return *m.state, nil
}
