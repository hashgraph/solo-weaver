// SPDX-License-Identifier: Apache-2.0

//go:build integration

package workflows

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// fakeUfwUnit stands in for the real ufw.service. The step probes by unit name,
// so any unit systemd can enable and start is enough — and this avoids installing
// a real firewall that would take over the VM's networking.
const fakeUfwUnit = `[Unit]
Description=Fake ufw for #982 preflight integration test

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`

// installFakeUfwUnit drops the fake unit into /etc/systemd/system and removes it
// afterwards. It skips if the host has a real ufw.service, so it can never
// clobber a genuine firewall manager.
func installFakeUfwUnit(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	if systemdKnowsUnit(t, "ufw.service") {
		t.Skip("host has a real ufw.service; refusing to shadow it")
	}

	path := filepath.Join("/etc/systemd/system", "ufw.service")
	require.NoError(t, os.WriteFile(path, []byte(fakeUfwUnit), 0o644))
	require.NoError(t, soos.DaemonReload(ctx))

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_ = soos.StopService(cleanupCtx, "ufw.service")
		_ = soos.DisableService(cleanupCtx, "ufw.service")
		_ = os.Remove(path)
		_ = soos.DaemonReload(cleanupCtx)
	})
}

// systemdKnowsUnit reports whether systemd already resolves a unit of this name.
func systemdKnowsUnit(t *testing.T, unit string) bool {
	t.Helper()
	for _, bin := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		return exec.CommandContext(context.Background(), bin, "cat", unit).Run() == nil
	}
	return false
}

// stubUfwConf writes /etc/ufw/ufw.conf with the given ENABLED value, restoring
// whatever was there before (including its absence).
func stubUfwConf(t *testing.T, enabled string) {
	t.Helper()
	const dir = "/etc/ufw"
	path := filepath.Join(dir, "ufw.conf")

	original, readErr := os.ReadFile(path)
	existed := readErr == nil
	dirExisted := true
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		dirExisted = false
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	require.NoError(t, os.WriteFile(path, []byte("# managed by an integration test\nENABLED="+enabled+"\n"), 0o644))

	t.Cleanup(func() {
		if existed {
			_ = os.WriteFile(path, original, 0o644)
			return
		}
		_ = os.Remove(path)
		if !dirExisted {
			_ = os.Remove(dir)
		}
	})
}

// runFirewallManagersStep builds and runs the production step — real systemd
// probes, real /etc/ufw/ufw.conf read, no seams injected.
func runFirewallManagersStep(t *testing.T) *automa.Report {
	t.Helper()
	step, err := CheckFirewallManagersStep().Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

// Test_CheckFirewallManagers_Integration exercises the #982 preflight against
// real systemd. The three subtests go together: report the manager when it is on,
// stay silent when it is off, and stay silent when it is on with no ruleset. A
// detector that always warns, or never warns, fails one of them.
func Test_CheckFirewallManagers_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	installFakeUfwUnit(t)
	ctx := context.Background()

	t.Run("an enabled and running manager is reported", func(t *testing.T) {
		stubUfwConf(t, "yes")
		require.NoError(t, soos.EnableService(ctx, "ufw.service"))
		require.NoError(t, soos.StartService(ctx, "ufw.service"))

		report := runFirewallManagersStep(t)

		// Advisory by design: the finding is reported, the install still proceeds.
		require.NoError(t, report.Error)
		require.Equal(t, automa.StatusSuccess, report.Status)
		require.Equal(t, "enabled, running", report.Metadata["ufw.service"])
	})

	t.Run("a stopped and disabled manager is not reported", func(t *testing.T) {
		stubUfwConf(t, "yes")
		require.NoError(t, soos.StopService(ctx, "ufw.service"))
		require.NoError(t, soos.DisableService(ctx, "ufw.service"))

		report := runFirewallManagersStep(t)

		require.NoError(t, report.Error)
		require.Empty(t, report.Metadata,
			"a host with no active firewall manager must produce no finding")
	})

	t.Run("a running manager with ENABLED=no loads no ruleset and is not reported", func(t *testing.T) {
		// Stock Ubuntu ships ufw.service enabled with ENABLED=no, which loads
		// nothing — warning there would train operators to ignore the check.
		stubUfwConf(t, "no")
		require.NoError(t, soos.EnableService(ctx, "ufw.service"))
		require.NoError(t, soos.StartService(ctx, "ufw.service"))

		report := runFirewallManagersStep(t)

		require.NoError(t, report.Error)
		require.Empty(t, report.Metadata)
	})
}
