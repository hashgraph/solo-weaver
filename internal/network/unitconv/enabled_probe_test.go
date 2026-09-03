// SPDX-License-Identifier: Apache-2.0

package unitconv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const probeService = "solo-provisioner-network-nft.service"

// TestEnabledAtBoot covers the enablement read that replaced the DBus query on the
// startup-migration path: the wants symlink decides, nothing else.
func TestEnabledAtBoot(t *testing.T) {
	t.Run("no wants directory reads as disabled", func(t *testing.T) {
		on, err := enabledAtBoot(filepath.Join(t.TempDir(), "multi-user.target.wants"), probeService)
		require.NoError(t, err)
		require.False(t, on)
	})

	t.Run("wants directory without the link reads as disabled", func(t *testing.T) {
		dir := t.TempDir()
		on, err := enabledAtBoot(dir, probeService)
		require.NoError(t, err)
		require.False(t, on)
	})

	t.Run("the enablement symlink reads as enabled", func(t *testing.T) {
		dir := t.TempDir()
		unit := filepath.Join(dir, "unit")
		require.NoError(t, os.WriteFile(unit, []byte("[Unit]\n"), 0o644))
		require.NoError(t, os.Symlink(unit, filepath.Join(dir, probeService)))
		on, err := enabledAtBoot(dir, probeService)
		require.NoError(t, err)
		require.True(t, on)
	})

	// A hand-removed unit file leaves the link dangling. That is still an
	// enablement record, and rewriting the unit is what repairs it.
	t.Run("a dangling enablement symlink still reads as enabled", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, probeService)))
		on, err := enabledAtBoot(dir, probeService)
		require.NoError(t, err)
		require.True(t, on)
	})
}

// TestEnabledAtBootProductionDir pins the wants directory: `systemctl enable` writes
// the link there, so a typo would make every host read as disabled forever.
func TestEnabledAtBootProductionDir(t *testing.T) {
	require.Equal(t, "/etc/systemd/system/multi-user.target.wants", multiUserWantsDir)
}
