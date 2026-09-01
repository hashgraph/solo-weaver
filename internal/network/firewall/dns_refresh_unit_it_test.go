// SPDX-License-Identifier: Apache-2.0

//go:build integration

package firewall

import (
	"context"
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// backupDNSRefreshUnits snapshots both unit files and the timer's enablement,
// and restores all three afterwards, so a run on a real host changes nothing.
// Mirrors backupNetworkNftUnit in internal/workflows/network_nft_unit_it_test.go.
func backupDNSRefreshUnits(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	type snapshot struct {
		path    string
		content []byte
		existed bool
	}
	var snaps []snapshot
	for _, path := range []string{DNSRefreshTimerUnitPath, DNSRefreshServiceUnitPath} {
		content, readErr := os.ReadFile(path)
		existed := readErr == nil
		if !existed {
			require.True(t, os.IsNotExist(readErr), "could not read %s: %v", path, readErr)
		}
		snaps = append(snaps, snapshot{path: path, content: content, existed: existed})
	}
	wasEnabled, _ := soos.IsServiceEnabled(ctx, DNSRefreshTimer)

	t.Cleanup(func() {
		restoreCtx := context.Background()
		for _, s := range snaps {
			if s.existed {
				_ = os.WriteFile(s.path, s.content, 0o644)
			} else {
				_ = os.Remove(s.path)
			}
		}
		_ = soos.DaemonReload(restoreCtx)
		switch {
		case snaps[0].existed && wasEnabled:
			_ = soos.EnableService(restoreCtx, DNSRefreshTimer)
		case !wasEnabled:
			_ = soos.DisableService(restoreCtx, DNSRefreshTimer)
		}
	})
}

// Test_DNSRefreshUnit_InstallEnableStart_Integration exercises SyncDNSRefreshTimer(true)
// against real systemd: the units land byte-identical to the embedded copies,
// systemd enables and arms the timer, and a second call is a no-op.
func Test_DNSRefreshUnit_InstallEnableStart_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}
	backupDNSRefreshUnits(t)
	ctx := context.Background()

	require.NoError(t, SyncDNSRefreshTimer(ctx, true))

	embeddedTimer, err := templates.Files.ReadFile(dnsRefreshTimerTemplate)
	require.NoError(t, err)
	embeddedService, err := templates.Files.ReadFile(dnsRefreshServiceTemplate)
	require.NoError(t, err)

	onDiskTimer, err := os.ReadFile(DNSRefreshTimerUnitPath)
	require.NoError(t, err)
	require.Equal(t, string(embeddedTimer), string(onDiskTimer))

	onDiskService, err := os.ReadFile(DNSRefreshServiceUnitPath)
	require.NoError(t, err)
	require.Equal(t, string(embeddedService), string(onDiskService))

	enabled, err := soos.IsServiceEnabled(ctx, DNSRefreshTimer)
	require.NoError(t, err)
	require.True(t, enabled, "the timer must be enabled, or it never fires at boot")

	running, err := soos.IsServiceRunning(ctx, DNSRefreshTimer)
	require.NoError(t, err)
	require.True(t, running, "a started timer is ActiveState=active while armed and waiting")

	// Second call: bytes are already current, so this must be a pure no-op that
	// still succeeds rather than erroring on an already-enabled, already-started unit.
	require.NoError(t, SyncDNSRefreshTimer(ctx, true))
}

// Test_DNSRefreshUnit_Remove_Integration exercises SyncDNSRefreshTimer(false),
// including the crash-recovery case a Copilot review caught on this PR: a
// prior partial write left only one of the two unit files on disk, and removal
// must still reach the other one instead of skipping on the first file's absence.
func Test_DNSRefreshUnit_Remove_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}
	backupDNSRefreshUnits(t)
	ctx := context.Background()

	t.Run("removes both units when both are present", func(t *testing.T) {
		require.NoError(t, SyncDNSRefreshTimer(ctx, true))
		require.NoError(t, SyncDNSRefreshTimer(ctx, false))

		require.NoFileExists(t, DNSRefreshTimerUnitPath)
		require.NoFileExists(t, DNSRefreshServiceUnitPath)

		enabled, err := soos.IsServiceEnabled(ctx, DNSRefreshTimer)
		require.NoError(t, err)
		require.False(t, enabled)
	})

	t.Run("removes the surviving unit when only one file is present", func(t *testing.T) {
		require.NoError(t, SyncDNSRefreshTimer(ctx, true))
		require.NoError(t, os.Remove(DNSRefreshTimerUnitPath))
		require.NoError(t, soos.DaemonReload(ctx))

		require.NoError(t, SyncDNSRefreshTimer(ctx, false))

		require.NoFileExists(t, DNSRefreshServiceUnitPath,
			"the service unit must still be removed even though the timer unit was already gone")
	})

	t.Run("is a no-op when neither file is present", func(t *testing.T) {
		require.NoFileExists(t, DNSRefreshTimerUnitPath)
		require.NoFileExists(t, DNSRefreshServiceUnitPath)
		require.NoError(t, SyncDNSRefreshTimer(ctx, false))
	})
}
