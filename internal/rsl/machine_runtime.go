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

// MachineRuntimeResolver manages the current machine state (software and hardware) for
// the local host. Unlike BlockNodeRuntimeResolver, it does not perform per-field
// EffectiveValue resolution across multiple sources (env vars, config file, user inputs,
// persisted state, reality). Machine state has a single authoritative source — the
// reality checker — so the resolver simply stores the latest snapshot returned by that
// checker and exposes it for direct reads.
//
// The With* methods (WithIntent, WithUserInputs, WithConfig, WithDefaults, WithEnv) are
// implemented as required by the Resolver interface but are no-ops here; they exist only
// to satisfy the generic contract shared with BlockNodeRuntimeResolver and
// ClusterRuntimeResolver.
type MachineRuntimeResolver struct {
	mu              sync.Mutex
	cfg             *models.Config
	state           *state.MachineState
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.MachineState]

	intent *models.Intent
	inputs *models.MachineInputs
}

func (m *MachineRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.MachineState, models.MachineInputs] {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.intent = &intent
	return m
}

func (m *MachineRuntimeResolver) WithUserInputs(inputs models.MachineInputs) Resolver[state.MachineState, models.MachineInputs] {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.inputs = &inputs
	return m
}

func (m *MachineRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.MachineState, models.MachineInputs] {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg = &cfg
	return m
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

func (m *MachineRuntimeResolver) WithState(st state.MachineState) Resolver[state.MachineState, models.MachineInputs] {
	m.mu.Lock()
	m.state = &st
	m.mu.Unlock()

	return m
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

// SoftwareState looks up the named component in the most recent machine state snapshot.
// Returns (zero, false) when the snapshot has not yet been synced (nil state or zero LastSync),
// signalling the caller to fall back to disk verification.
// Returns (zero, false) when the component is absent from the snapshot (unrecognised name).
// Returns (sw, true) when the snapshot is available and the component is present.
func (m *MachineRuntimeResolver) SoftwareState(name string) (state.SoftwareState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil || m.state.LastSync.IsZero() {
		return state.SoftwareState{}, false
	}

	sw, ok := m.state.Software[name]
	if !ok {
		return state.SoftwareState{}, false
	}

	if sw.Name == "" {
		sw.Name = name
	}

	return sw, true
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
