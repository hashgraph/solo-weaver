// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkNftUnitMigration_Metadata(t *testing.T) {
	m := NewNetworkNftUnitMigration()
	assert.Equal(t, "network-nft-loader-unit", m.ID())
	assert.Contains(t, m.Description(), "solo-provisioner-network-nft.service")
}

// stubProbe stubs the unit-probe seam and pins the caller as root.
func stubProbe(t *testing.T, needsConverge bool, err error) {
	t.Helper()
	orig := networkNftUnitNeedsConverge
	t.Cleanup(func() { networkNftUnitNeedsConverge = orig })
	networkNftUnitNeedsConverge = func(context.Context) (bool, error) { return needsConverge, err }
	stubEuid(t, 0)
}

// stubEuid pins the effective uid Applies sees.
func stubEuid(t *testing.T, euid int) {
	t.Helper()
	orig := nftUnitGeteuid
	t.Cleanup(func() { nftUnitGeteuid = orig })
	nftUnitGeteuid = func() int { return euid }
}

func TestNetworkNftUnitMigration_Applies(t *testing.T) {
	tests := []struct {
		name          string
		needsConverge bool
		err           error
		want          bool
	}{
		{"host already on the current unit, enabled, is skipped", false, nil, false},
		// True covers both drift shapes the probe folds together: wrong bytes,
		// and byte-current-but-disabled.
		{"host needing the unit converged is picked up", true, nil, true},
		// A probe error must never propagate to RunStartupMigrations.
		{"probe failure is skipped, not fatal", true, errorx.ExternalError.New("boom"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubProbe(t, tc.needsConverge, tc.err)
			got, err := NewNetworkNftUnitMigration().Applies(newMctx("0.28.1", "0.29.0"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNetworkNftUnitMigration_AppliesSkipsUnprivilegedCaller checks that a
// non-root caller skips without probing.
func TestNetworkNftUnitMigration_AppliesSkipsUnprivilegedCaller(t *testing.T) {
	probed := false
	stubProbe(t, true, nil)
	networkNftUnitNeedsConverge = func(context.Context) (bool, error) {
		probed = true
		return true, nil
	}
	stubEuid(t, 1000) // after stubProbe, which pins root

	got, err := NewNetworkNftUnitMigration().Applies(newMctx("0.28.1", "0.29.0"))
	require.NoError(t, err)
	assert.False(t, got, "an unprivileged caller must not attempt the /usr/lib write")
	assert.False(t, probed, "the unit probe must be short-circuited by the privilege gate")
}

// TestNetworkNftUnitMigration_AppliesIsVersionIndependent checks Applies fires
// regardless of the installed/current version pair.
func TestNetworkNftUnitMigration_AppliesIsVersionIndependent(t *testing.T) {
	stubProbe(t, true, nil)
	m := NewNetworkNftUnitMigration()

	for _, mctx := range []struct {
		installed, current string
	}{
		{"0.28.1", "0.29.0"}, // upgrade
		{"0.29.0", "0.29.0"}, // same version, re-run
		{"", "0.29.0"},       // no recorded version
	} {
		got, err := m.Applies(newMctx(mctx.installed, mctx.current))
		require.NoError(t, err)
		assert.True(t, got, "installed=%q current=%q", mctx.installed, mctx.current)
	}
}

func TestNetworkNftUnitMigration_Execute(t *testing.T) {
	orig := ensureNetworkNftUnit
	t.Cleanup(func() { ensureNetworkNftUnit = orig })

	t.Run("converges the unit", func(t *testing.T) {
		calls := 0
		ensureNetworkNftUnit = func(_ context.Context) error { calls++; return nil }
		require.NoError(t, NewNetworkNftUnitMigration().Execute(context.Background(), newMctx("0.28.1", "0.29.0")))
		assert.Equal(t, 1, calls)
	})

	// A failure must not propagate; it would fail every command on the host.
	t.Run("a failure is warned about, not returned", func(t *testing.T) {
		ensureNetworkNftUnit = func(_ context.Context) error { return errorx.ExternalError.New("read-only fs") }
		require.NoError(t, NewNetworkNftUnitMigration().Execute(context.Background(), newMctx("0.28.1", "0.29.0")))
	})
}

func TestNetworkNftUnitMigration_RollbackIsNoOp(t *testing.T) {
	require.NoError(t, NewNetworkNftUnitMigration().Rollback(context.Background(), newMctx("0.28.1", "0.29.0")))
}
