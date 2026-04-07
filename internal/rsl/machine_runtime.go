// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

type MachineRuntimeResolver struct {
	mu              sync.Mutex
	cfg             *models.Config
	state           *state.MachineState
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.MachineState]

	intent *models.Intent
	inputs *models.MachineInputs
}

func (c *MachineRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.MachineState, models.MachineInputs] {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.intent = &intent
	return c
}

func (c *MachineRuntimeResolver) WithUserInputs(inputs models.MachineInputs) Resolver[state.MachineState, models.MachineInputs] {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.inputs = &inputs
	return c
}

func (c *MachineRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.MachineState, models.MachineInputs] {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cfg = &cfg
	return c
}

// WithDefaults is a no-op stub satisfying the Resolver interface.
// MachineRuntimeResolver has no field-level EffectiveValue resolvers yet.
func (m *MachineRuntimeResolver) WithDefaults(_ models.Config) Resolver[state.MachineState, models.MachineInputs] {
	return m
}

// WithEnv is a no-op stub satisfying the Resolver interface.
// MachineRuntimeResolver has no field-level EffectiveValue resolvers yet,
// so env var injection is not applicable.
func (m *MachineRuntimeResolver) WithEnv(_ models.Config) Resolver[state.MachineState, models.MachineInputs] {
	return m
}

func (c *MachineRuntimeResolver) WithState(st state.MachineState) Resolver[state.MachineState, models.MachineInputs] {
	c.mu.Lock()
	c.state = &st
	c.mu.Unlock()

	return c
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

// SoftwareState returns the effective SoftwareState for the named software component.
// Returns (zero, false) when machine state has not been initialized yet; the caller
// should fall back to disk verification in that case.
// Returns (state, true) once machine state is available — even if the name is absent
// from the map (meaning the component is not installed/configured per RSL state).
func (m *MachineRuntimeResolver) SoftwareState(name string) (state.SoftwareState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return state.SoftwareState{}, false
	}
	return m.state.Software[name], true
}

func (m *MachineRuntimeResolver) CurrentState() (state.MachineState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		return state.NewMachineState(), errorx.IllegalState.New("machine state is not initialized")
	}

	return *m.state, nil
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
	return &MachineRuntimeResolver{
		cfg:             &cfg,
		state:           &clusterState,
		realityChecker:  realityChecker,
		refreshInterval: refreshInterval,
	}, nil
}
