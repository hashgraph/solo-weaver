// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrioHostLinksMigration_Metadata(t *testing.T) {
	m := NewCrioHostLinksMigration()
	assert.Equal(t, "crio-host-links-v"+crioHostLinksMinVersion, m.ID())
	assert.Contains(t, m.Description(), "crictl")
}

func TestCrioHostLinksMigration_Applies(t *testing.T) {
	m := NewCrioHostLinksMigration()

	tests := []struct {
		name      string
		installed string
		current   string
		want      bool
	}{
		{"upgrade across boundary applies", "0.36.0", crioHostLinksMinVersion, true},
		{"fresh install does not apply", "", crioHostLinksMinVersion, false},
		{"already past boundary does not apply", crioHostLinksMinVersion, "0.37.0", false},
		{"below boundary on both sides does not apply", "0.35.0", "0.36.0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCrioHostLinksMigration_Execute(t *testing.T) {
	origInstalled := crioInstalled
	origPresent := crioHostLinksPresent
	origLink := linkCrioToHost
	t.Cleanup(func() {
		crioInstalled = origInstalled
		crioHostLinksPresent = origPresent
		linkCrioToHost = origLink
	})

	m := NewCrioHostLinksMigration()
	mctx := newMctx("0.36.0", crioHostLinksMinVersion)

	var linked bool
	linkCrioToHost = func() error { linked = true; return nil }

	t.Run("skips when cri-o is not installed", func(t *testing.T) {
		linked = false
		crioInstalled = func() (bool, error) { return false, nil }
		crioHostLinksPresent = func() (bool, error) { return false, nil }
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, linked)
	})

	t.Run("skips on install probe error", func(t *testing.T) {
		linked = false
		crioInstalled = func() (bool, error) { return false, assert.AnError }
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, linked)
	})

	t.Run("skips on links probe error", func(t *testing.T) {
		linked = false
		crioInstalled = func() (bool, error) { return true, nil }
		crioHostLinksPresent = func() (bool, error) { return false, assert.AnError }
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, linked)
	})

	t.Run("no-ops when links already present", func(t *testing.T) {
		linked = false
		crioInstalled = func() (bool, error) { return true, nil }
		crioHostLinksPresent = func() (bool, error) { return true, nil }
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, linked)
	})

	t.Run("creates links when missing", func(t *testing.T) {
		linked = false
		crioInstalled = func() (bool, error) { return true, nil }
		crioHostLinksPresent = func() (bool, error) { return false, nil }
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.True(t, linked)
	})

	t.Run("returns error when linking fails", func(t *testing.T) {
		crioInstalled = func() (bool, error) { return true, nil }
		crioHostLinksPresent = func() (bool, error) { return false, nil }
		linkCrioToHost = func() error { return assert.AnError }
		require.Error(t, m.Execute(context.Background(), mctx))
	})
}
