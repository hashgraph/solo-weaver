// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package rsl

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	htime "helm.sh/helm/v3/pkg/time"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockMachineChecker is a controllable reality.Checker[state.MachineState].
type mockMachineChecker struct {
	state state.MachineState
	err   error
	calls int
}

func (m *mockMachineChecker) RefreshState(_ context.Context) (state.MachineState, error) {
	m.calls++
	return m.state, m.err
}

// ── Fixtures ──────────────────────────────────────────────────────────────────

// syncedMachineState returns a MachineState with a non-zero LastSync and a named software entry.
func syncedMachineState(software map[string]state.SoftwareState) state.MachineState {
	ms := state.NewMachineState()
	ms.LastSync = htime.Now()
	for k, v := range software {
		ms.Software[k] = v
	}
	return ms
}

// newTestMachineResolver builds a MachineRuntimeResolver seeded with the given state.
func newTestMachineResolver(ms state.MachineState) *MachineRuntimeResolver {
	r, _ := NewMachineRuntimeResolver(
		models.Config{},
		ms,
		&mockMachineChecker{},
		time.Minute,
	)
	return r.(*MachineRuntimeResolver)
}

// ── SoftwareState tests ───────────────────────────────────────────────────────

func TestMachineRuntimeResolver_SoftwareState_NilState(t *testing.T) {
	r := &MachineRuntimeResolver{} // state is nil
	sw, ok := r.SoftwareState("kubectl")
	assert.False(t, ok, "nil state should return ok=false")
	assert.Equal(t, state.SoftwareState{}, sw)
}

func TestMachineRuntimeResolver_SoftwareState_ZeroLastSync(t *testing.T) {
	ms := state.NewMachineState() // LastSync is zero
	ms.Software["kubectl"] = state.SoftwareState{Installed: true}
	r := newTestMachineResolver(ms)
	r.state.LastSync = htime.Time{} // force zero sync time

	sw, ok := r.SoftwareState("kubectl")
	assert.False(t, ok, "zero LastSync should return ok=false (state not yet synced)")
	assert.Equal(t, state.SoftwareState{}, sw)
}

func TestMachineRuntimeResolver_SoftwareState_ComponentPresent(t *testing.T) {
	want := state.SoftwareState{
		Name:       "kubectl",
		Version:    "1.30.0",
		Installed:  true,
		Configured: true,
	}
	r := newTestMachineResolver(syncedMachineState(map[string]state.SoftwareState{
		"kubectl": want,
	}))

	got, ok := r.SoftwareState("kubectl")
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestMachineRuntimeResolver_SoftwareState_NameBackfill(t *testing.T) {
	// Stored entry has empty Name (common when map key is the name).
	r := newTestMachineResolver(syncedMachineState(map[string]state.SoftwareState{
		"kubectl": {Installed: true}, // Name is empty
	}))

	got, ok := r.SoftwareState("kubectl")
	require.True(t, ok)
	assert.Equal(t, "kubectl", got.Name, "Name should be backfilled from the map key")
}

func TestMachineRuntimeResolver_SoftwareState_ComponentAbsent(t *testing.T) {
	r := newTestMachineResolver(syncedMachineState(map[string]state.SoftwareState{
		"kubectl": {Installed: true},
	}))

	sw, ok := r.SoftwareState("kubelet")
	assert.False(t, ok, "absent component should return ok=false")
	assert.Equal(t, state.SoftwareState{}, sw)
}

func TestMachineRuntimeResolver_SoftwareState_NotInstalled(t *testing.T) {
	r := newTestMachineResolver(syncedMachineState(map[string]state.SoftwareState{
		"kubectl": {Installed: false, Configured: false},
	}))

	got, ok := r.SoftwareState("kubectl")
	require.True(t, ok, "present-but-uninstalled component should still return ok=true")
	assert.False(t, got.Installed)
	assert.False(t, got.Configured)
}

func TestMachineRuntimeResolver_SoftwareState_EmptySoftwareMap(t *testing.T) {
	r := newTestMachineResolver(syncedMachineState(nil))

	sw, ok := r.SoftwareState("kubectl")
	assert.False(t, ok)
	assert.Equal(t, state.SoftwareState{}, sw)
}
