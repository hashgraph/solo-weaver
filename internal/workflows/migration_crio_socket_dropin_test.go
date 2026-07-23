// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrioSocketDropInMigration_Metadata(t *testing.T) {
	m := NewCrioSocketDropInMigration()
	assert.Equal(t, "crio-socket-dropin-v"+crioSocketDropInMinVersion, m.ID())
	assert.Contains(t, m.Description(), "crio-images")
}

func TestCrioSocketDropInMigration_Applies(t *testing.T) {
	m := NewCrioSocketDropInMigration()

	tests := []struct {
		name      string
		installed string
		current   string
		want      bool
	}{
		{"upgrade across boundary applies", "0.24.0", crioSocketDropInMinVersion, true},
		{"upgrade from below to above applies", "0.0.0", "0.26.0", true},
		{"fresh install does not apply", "", crioSocketDropInMinVersion, false},
		{"already past boundary does not apply", crioSocketDropInMinVersion, "0.26.0", false},
		{"below boundary on both sides does not apply", "0.23.0", "0.24.0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCrioSocketDropInMigration_Execute(t *testing.T) {
	origInstalled := crioInstalled
	origPresent := crioSocketBridgePresent
	origReconfigure := reconfigureCrioSocketDropIn
	origEnsure := ensureCrioSocketSymlink
	origReload := crioDaemonReload
	t.Cleanup(func() {
		crioInstalled = origInstalled
		crioSocketBridgePresent = origPresent
		reconfigureCrioSocketDropIn = origReconfigure
		ensureCrioSocketSymlink = origEnsure
		crioDaemonReload = origReload
	})

	m := NewCrioSocketDropInMigration()
	mctx := newMctx("0.24.0", crioSocketDropInMinVersion)

	// applyTracker wires the apply seams and records whether each ran.
	type tracker struct{ reconfigured, reloaded, symlinked bool }
	wire := func(tr *tracker) {
		reconfigureCrioSocketDropIn = func() error { tr.reconfigured = true; return nil }
		crioDaemonReload = func(context.Context) error { tr.reloaded = true; return nil }
		ensureCrioSocketSymlink = func() error { tr.symlinked = true; return nil }
	}

	t.Run("skips when cri-o is not installed", func(t *testing.T) {
		var tr tracker
		crioInstalled = func() (bool, error) { return false, nil }
		crioSocketBridgePresent = func() (bool, error) { return false, nil }
		wire(&tr)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, tr.reconfigured, "must not write the drop-in when cri-o is not installed")
	})

	t.Run("skips on cri-o install probe error", func(t *testing.T) {
		var tr tracker
		crioInstalled = func() (bool, error) { return false, assert.AnError }
		wire(&tr)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, tr.reconfigured, "a probe error must be a soft skip, not a failure")
	})

	// Remaining cases assume cri-o is installed.
	crioInstalled = func() (bool, error) { return true, nil }

	t.Run("skips when the drop-in is already present", func(t *testing.T) {
		var tr tracker
		crioSocketBridgePresent = func() (bool, error) { return true, nil }
		wire(&tr)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, tr.reconfigured, "must be idempotent when the drop-in already exists")
		assert.False(t, tr.reloaded)
		assert.False(t, tr.symlinked)
	})

	t.Run("applies drop-in, reload, and symlink when missing", func(t *testing.T) {
		var tr tracker
		crioSocketBridgePresent = func() (bool, error) { return false, nil }
		wire(&tr)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.True(t, tr.reconfigured, "must write the drop-in")
		assert.True(t, tr.reloaded, "must daemon-reload so the override persists across restarts")
		assert.True(t, tr.symlinked, "must create the default-path socket symlink for immediate effect")
	})

	t.Run("returns error when writing the drop-in fails", func(t *testing.T) {
		crioSocketBridgePresent = func() (bool, error) { return false, nil }
		reconfigureCrioSocketDropIn = func() error { return assert.AnError }
		crioDaemonReload = func(context.Context) error { return nil }
		ensureCrioSocketSymlink = func() error { return nil }
		err := m.Execute(context.Background(), mctx)
		require.Error(t, err)
	})
}
