// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package shape

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// backupShaperInstall snapshots the installed unit, the boot script and the unit's
// enablement, and restores all three afterwards, so a run on a provisioned host
// changes nothing.
func backupShaperInstall(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	unit, unitErr := os.ReadFile(TcEgressServiceUnitPath)
	unitExisted := unitErr == nil
	if !unitExisted {
		require.True(t, os.IsNotExist(unitErr), "could not read %s: %v", TcEgressServiceUnitPath, unitErr)
	}
	script, scriptErr := os.ReadFile(TcEgressScriptPath)
	scriptExisted := scriptErr == nil
	if !scriptExisted {
		require.True(t, os.IsNotExist(scriptErr), "could not read %s: %v", TcEgressScriptPath, scriptErr)
	}
	wasEnabled, _ := soos.IsServiceEnabled(ctx, TcEgressService)

	t.Cleanup(func() {
		restoreCtx := context.Background()
		if unitExisted {
			_ = os.WriteFile(TcEgressServiceUnitPath, unit, 0o644)
		} else {
			_ = os.Remove(TcEgressServiceUnitPath)
		}
		if scriptExisted {
			_ = os.WriteFile(TcEgressScriptPath, script, 0o755)
		} else {
			_ = os.Remove(TcEgressScriptPath)
		}
		_ = soos.DaemonReload(restoreCtx)
		switch {
		case unitExisted && wasEnabled:
			_ = soos.EnableService(restoreCtx, TcEgressService)
		case !wasEnabled:
			_ = soos.DisableService(restoreCtx, TcEgressService)
		}
	})
}

// writeBootScript installs a stand-in boot script at the production path. Its
// contents do not matter: nothing here starts the unit.
func writeBootScript(t *testing.T) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(TcEgressScriptPath), 0o755))
	require.NoError(t, os.WriteFile(TcEgressScriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
}

// Test_RemoveTcEgressUnit_Integration covers the teardown counterpart of
// EnsureTcEgressUnit against real systemd. The boot script is removed first
// because it is the guard TcEgressUnitNeedsConverge keys on — leaving it behind
// would have the startup migration reinstall the unit teardown just removed.
func Test_RemoveTcEgressUnit_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}
	backupShaperInstall(t)

	ctx := context.Background()

	t.Run("removes the unit, the boot script and the enablement", func(t *testing.T) {
		require.NoError(t, EnsureTcEgressUnit(ctx))
		writeBootScript(t)

		enabled, err := soos.IsServiceEnabled(ctx, TcEgressService)
		require.NoError(t, err)
		require.True(t, enabled, "precondition: the unit must be installed and enabled")

		require.NoError(t, RemoveTcEgressUnit(ctx))

		require.NoFileExists(t, TcEgressServiceUnitPath)
		require.NoFileExists(t, TcEgressScriptPath)

		enabled, err = soos.IsServiceEnabled(ctx, TcEgressService)
		require.NoError(t, err)
		require.False(t, enabled, "a removed unit must not be left enabled at boot")

		// The property the removal order exists for: with the guard gone, the
		// startup migration must not reinstall what teardown just removed.
		needsConverge, err := TcEgressUnitNeedsConverge()
		require.NoError(t, err)
		require.False(t, needsConverge,
			"a torn-down host still reads as needing a boot unit: the guard survived teardown")
	})

	// A partial teardown, or a host whose unit was removed by hand, must not keep
	// the guard: it would reinstall the unit on the next invocation.
	t.Run("an orphaned boot script is removed even with no unit", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(TcEgressServiceUnitPath))
		require.NoError(t, soos.DaemonReload(ctx))
		writeBootScript(t)

		require.NoError(t, RemoveTcEgressUnit(ctx))

		require.NoFileExists(t, TcEgressScriptPath)
		needsConverge, err := TcEgressUnitNeedsConverge()
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	t.Run("a host with nothing installed is a no-op", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(TcEgressServiceUnitPath))
		require.NoError(t, os.RemoveAll(TcEgressScriptPath))
		require.NoError(t, soos.DaemonReload(ctx))

		require.NoError(t, RemoveTcEgressUnit(ctx), "teardown must be idempotent")
	})
}
