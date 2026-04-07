//go:build integration

// SPDX-License-Identifier: Apache-2.0

package reality_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStateManager creates a real state.Manager backed by a temp directory.
func newTestStateManager(t *testing.T) state.Manager {
	t.Helper()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	sm, err := state.NewStateManager(state.WithStateFile(stateFile))
	require.NoError(t, err)
	err = sm.Refresh()
	require.NoError(t, err)
	err = sm.FlushAll()
	require.NoError(t, err)
	return sm
}

func TestMachineChecker_Integration_RefreshState(t *testing.T) {
	sm := newTestStateManager(t)
	checker, err := reality.NewMachineChecker(sm, "", "")
	require.NoError(t, err)

	ms, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	t.Run("LastSync is set", func(t *testing.T) {
		assert.False(t, ms.LastSync.IsZero(), "LastSync should be set after RefreshState")
	})

	t.Run("Software map is populated", func(t *testing.T) {
		assert.NotNil(t, ms.Software, "Software map must not be nil")
		// At least one software entry should be present because Installers() is non-empty
		assert.NotEmpty(t, ms.Software, "Software map should have at least one entry")
	})

	t.Run("Hardware map contains expected keys", func(t *testing.T) {
		require.NotNil(t, ms.Hardware)
		for _, key := range []string{"os", "cpu", "memory", "storage"} {
			hw, ok := ms.Hardware[key]
			assert.Truef(t, ok, "hardware key %q should be present", key)
			assert.NotEmpty(t, hw.Type, "Type should be non-empty for key %q", key)
		}
	})

	t.Run("CPU hardware has positive count", func(t *testing.T) {
		cpu, ok := ms.Hardware["cpu"]
		require.True(t, ok)
		assert.Greater(t, cpu.Count, 0, "CPU count should be > 0")
	})

	t.Run("Memory hardware has size info", func(t *testing.T) {
		mem, ok := ms.Hardware["memory"]
		require.True(t, ok)
		assert.NotEmpty(t, mem.Size, "Memory Size should be set")
		assert.NotEmpty(t, mem.Info, "Memory Info (available) should be set")
	})

	t.Run("OS hardware has non-empty info", func(t *testing.T) {
		osHW, ok := ms.Hardware["os"]
		require.True(t, ok)
		assert.NotEmpty(t, osHW.Info, "OS info should contain vendor and version")
	})

	t.Run("Storage hardware has size", func(t *testing.T) {
		st, ok := ms.Hardware["storage"]
		require.True(t, ok)
		assert.NotEmpty(t, st.Size, "Storage Size should be set")
	})
}

func TestMachineChecker_Integration_SoftwareState(t *testing.T) {
	sm := newTestStateManager(t)
	checker, err := reality.NewMachineChecker(sm, "", "")
	require.NoError(t, err)

	ms, err := checker.RefreshState(context.Background())
	require.NoError(t, err)

	for name, sw := range ms.Software {
		sw := sw // capture
		t.Run("software/"+name, func(t *testing.T) {
			assert.NotEmpty(t, sw.Version, "Version should be set for %q", name)
			assert.False(t, sw.LastSync.IsZero(), "LastSync should be set for %q", name)
		})
	}
}
