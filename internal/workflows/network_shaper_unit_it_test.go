// SPDX-License-Identifier: Apache-2.0

//go:build integration

package workflows

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// tcEgressUnitTemplate mirrors the private const of the same name in
// internal/network/shape; the tests compare the installed unit against it.
const tcEgressUnitTemplate = "files/network/solo-provisioner-bandwidth-shaper.service"

// staleShaperUnit is the pre-#980 unit an upgraded host has on disk: ordered before
// the network is configured, with no retry.
const staleShaperUnit = `[Unit]
Description=Solo Provisioner Bandwidth Shaper (tc HTB egress)
DefaultDependencies=no
After=network-pre.target
Before=network.target solo-provisioner-daemon.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`

// forcedRestartStatuses decodes the RestartForceExitStatus property, which systemd
// exposes as a pair of arrays (exit statuses, signals) rather than a string.
func forcedRestartStatuses(t *testing.T, value any) []int32 {
	t.Helper()
	pair, ok := value.([]any)
	require.True(t, ok, "RestartForceExitStatus is %T, not a status/signal pair", value)
	require.Len(t, pair, 2, "RestartForceExitStatus is not a (statuses, signals) pair: %v", pair)
	statuses, ok := pair[0].([]int32)
	require.True(t, ok, "forced exit statuses are %T, not a list of ints", pair[0])
	return statuses
}

// Test_ShaperUnitOrdering_Integration pins the #980 ordering and retry as systemd
// resolves them, not as the template spells them.
func Test_ShaperUnitOrdering_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupUnit(t, shape.TcEgressServiceUnitPath, shape.TcEgressService)

	require.NoError(t, shape.EnsureTcEgressUnit(context.Background()))

	after, before := unitOrdering(t, shape.TcEgressService)

	require.Contains(t, after, "network-online.target",
		"the replay must run once the links are up, or a netplan-created bond does not exist yet")
	// Ordering is transitive: this edge would make every daemon start wait on
	// network-online.target, and it guards nothing in return.
	require.NotContains(t, before, "solo-provisioner-daemon.service",
		"the shaper must not gate the daemon's start on network-online.target")

	// The pre-#980 ordering: the replay ran before anything configured a device.
	require.NotContains(t, after, "network-pre.target")
	require.NotContains(t, before, "network.target",
		"ordering before network.target cannot coexist with After=network-online.target")

	// Ordering alone is inert: without Wants= nothing pulls the target in.
	props := unitProperties(t, shape.TcEgressService)
	require.Contains(t, stringList(t, props, "Wants"), "network-online.target",
		"the unit must pull network-online.target in, or the ordering is never exercised")

	// The retry is the script's wait loop, not a restart policy — read back off
	// systemd, since that is where the RestartForceExitStatus= no-op is invisible.
	require.Equal(t, "no", props["Restart"],
		"a blanket restart would rebuild the root qdisc forever on a permanently bad config")
	require.Empty(t, forcedRestartStatuses(t, props["RestartForceExitStatus"]),
		"the unit must not rest its retry on RestartForceExitStatus=: it parses on every version "+
			"but only acts from systemd 256 (systemd#31148), and the supported hosts are older")
	require.Equal(t, true, props["RemainAfterExit"],
		"active (exited) is the operator's 'shaping applied' signal")

	// Read the budget back from systemd: a dropped directive or a 0 would leave the
	// script's wait loop never waiting.
	budget := waitBudgetFromEnvironment(t, stringList(t, props, "Environment"))
	require.Positive(t, budget, "the unit must hand the script a usable device-wait budget")

	// Type=oneshot disables the start timeout by default, so the unit has to set one
	// or a hung wait is unbounded — and it must outlast the wait it granted.
	timeoutUSec, ok := props["TimeoutStartUSec"].(uint64)
	require.True(t, ok, "TimeoutStartUSec is %T, not a uint64", props["TimeoutStartUSec"])
	timeout := time.Duration(timeoutUSec) * time.Microsecond
	require.NotEqual(t, uint64(math.MaxUint64), timeoutUSec,
		"systemd left the start timeout at the Type=oneshot default of infinity")
	require.Greater(t, timeout, time.Duration(budget)*time.Second,
		"TimeoutStartSec= must outlast the device-wait budget, or systemd kills the wait it granted")

	// Nothing restarts this unit automatically, so a start limit could only ever
	// refuse an operator's `network shape` command.
	interval, ok := props["StartLimitIntervalUSec"].(uint64)
	require.True(t, ok, "StartLimitIntervalUSec is %T, not a uint64", props["StartLimitIntervalUSec"])
	require.Zero(t, interval, "with no restart policy the start limit can only penalise an operator")
}

// waitBudgetFromEnvironment returns the SHAPER_DEVICE_WAIT_SECS seconds systemd
// parsed out of the unit's Environment=, or fails if it is absent.
func waitBudgetFromEnvironment(t *testing.T, env []string) int {
	t.Helper()
	prefix := shape.DeviceWaitEnvVar + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			budget, err := strconv.Atoi(strings.TrimPrefix(entry, prefix))
			require.NoError(t, err, "%s must be an integer number of seconds", entry)
			return budget
		}
	}
	require.Failf(t, "device-wait budget absent",
		"systemd parsed no %s out of the unit: %v", prefix, env)
	return 0
}

// Test_ShaperUnitIsSatisfiable_Integration checks the ordering is reachable at
// all — systemd resolves a cycle by silently dropping a job.
func Test_ShaperUnitIsSatisfiable_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupUnit(t, shape.TcEgressServiceUnitPath, shape.TcEgressService)
	backupUnit(t, firewall.NetworkNftServiceUnitPath, firewall.NetworkNftService)
	installManagerStubs(t)

	ctx := context.Background()
	require.NoError(t, shape.EnsureTcEgressUnit(ctx))
	require.NoError(t, firewall.EnsureNetworkNftUnit(ctx))

	bin := ""
	for _, c := range systemdAnalyzeCandidates {
		if _, err := os.Stat(c); err == nil {
			bin = c
			break
		}
	}
	if bin == "" {
		t.Skipf("systemd-analyze not found in %v", systemdAnalyzeCandidates)
	}

	units := append([]string{
		shape.TcEgressServiceUnitPath,
		firewall.NetworkNftServiceUnitPath,
		"network-online.target",
	}, firewallManagerUnits...)

	out := verifyOrdering(t, bin, units...)
	require.NotContains(t, out, "ordering cycle",
		"systemd-analyze verify reported an ordering cycle:\n%s", out)
}

// Test_ShaperUnitMigration_Integration exercises the #980 delivery path against real
// systemd: with no `network shape` mutation, the migration is the only converger.
func Test_ShaperUnitMigration_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupUnit(t, shape.TcEgressServiceUnitPath, shape.TcEgressService)

	ctx := context.Background()
	path := shape.TcEgressServiceUnitPath
	embedded, err := templates.Files.ReadFile(tcEgressUnitTemplate)
	require.NoError(t, err)

	m := NewNetworkShaperUnitMigration()
	mctx := newMctx("0.28.1", "0.29.0")

	t.Run("a stale unit is rewritten, reloaded and enabled", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte(staleShaperUnit), 0o644))
		require.NoError(t, soos.DaemonReload(ctx))

		// Precondition: systemd holds the stale ordering, so the assertions below
		// cannot be met by an earlier test's write.
		after, _ := unitOrdering(t, shape.TcEgressService)
		require.NotContains(t, after, "network-online.target")

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.True(t, applies, "a stale unit must be detected as drift")

		require.NoError(t, m.Execute(ctx, mctx))

		current, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(embedded), string(current),
			"the converged unit must be the embedded copy byte-for-byte")

		// Read back through systemd: proof Execute daemon-reloaded, not just wrote.
		after, before := unitOrdering(t, shape.TcEgressService)
		require.Contains(t, after, "network-online.target")
		// The stale unit carried this edge, so its absence proves the replacement.
		require.NotContains(t, before, "solo-provisioner-daemon.service")

		enabled, err := soos.IsServiceEnabled(ctx, shape.TcEgressService)
		require.NoError(t, err)
		require.True(t, enabled, "the converged unit must be enabled, or it never runs at boot")
	})

	t.Run("the converged host is left alone on the next invocation", func(t *testing.T) {
		// Set the precondition here, so a -run filter on this subtest alone cannot
		// pass vacuously.
		require.NoError(t, os.WriteFile(path, embedded, 0o644))
		require.NoError(t, soos.DaemonReload(ctx))
		require.NoError(t, soos.EnableService(ctx, shape.TcEgressService))

		needsConverge, err := shape.TcEgressUnitNeedsConverge()
		require.NoError(t, err)
		require.False(t, needsConverge)

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.False(t, applies, "the migration must not re-fire on a converged host")
	})

	// The failure a byte compare cannot see: the embedded copy on disk, disabled.
	t.Run("a current but disabled unit is re-enabled", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, embedded, 0o644))
		require.NoError(t, soos.DaemonReload(ctx))
		require.NoError(t, soos.DisableService(ctx, shape.TcEgressService))

		enabled, err := soos.IsServiceEnabled(ctx, shape.TcEgressService)
		require.NoError(t, err)
		require.False(t, enabled, "precondition: systemd must be holding the unit disabled")

		needsConverge, err := shape.TcEgressUnitNeedsConverge()
		require.NoError(t, err)
		require.True(t, needsConverge, "a byte-current unit that will not run at boot is drift")

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.True(t, applies)

		require.NoError(t, m.Execute(ctx, mctx))

		enabled, err = soos.IsServiceEnabled(ctx, shape.TcEgressService)
		require.NoError(t, err)
		require.True(t, enabled, "Execute must enable a unit it did not need to rewrite")

		// The bytes were already current, so nothing may have been rewritten.
		current, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(embedded), string(current))
	})

	t.Run("a host that never shaped traffic gets no boot unit", func(t *testing.T) {
		if _, err := os.Stat(shape.TcEgressScriptPath); err == nil {
			t.Skipf("host has a persisted boot script (%s); it is provisioned", shape.TcEgressScriptPath)
		}
		// RemoveAll, not Remove: under a -run filter the unit was never written.
		require.NoError(t, os.RemoveAll(path))
		require.NoError(t, soos.DaemonReload(ctx))

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.False(t, applies,
			"a host with no unit and no persisted boot script must not get a boot unit installed")
		require.NoFileExists(t, path)
	})
}

// ── Below: does the mechanism the unit rests on actually work, against scratch
// units rather than the real one? ─────────────────────────────────────────────

// waitProbeUnit mirrors the shaper's [Service] block on a script that waits for a
// sentinel file, down to the variable and exit status the real pair agree on.
// Format args: budget (s), start timeout (s), sentinel path.
var waitProbeUnit = `[Unit]
Description=Device-wait probe (#980 verification)
DefaultDependencies=no
StartLimitIntervalSec=0

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=` + shape.DeviceWaitEnvVar + `=%d
TimeoutStartSec=%ds
ExecStart=/bin/sh -c 'W=0; while [ ! -e %s ]; do [ "$W" -ge "$` + shape.DeviceWaitEnvVar +
	`" ] && exit ` + strconv.Itoa(shape.DeviceMissingExitCode) + `; sleep 1; W=$((W+1)); done'
`

const waitProbeName = "solo-weaver-device-wait-probe.service"

// installScratchUnit writes a throwaway unit under /etc/systemd/system, then stops
// and removes it. Stop, not just remove: it cancels any pending job.
func installScratchUnit(t *testing.T, name, content string) {
	t.Helper()

	path := filepath.Join("/etc/systemd/system", name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	t.Cleanup(func() {
		restoreCtx := context.Background()
		_ = soos.StopService(restoreCtx, name)
		_ = os.Remove(path)
		_ = soos.DaemonReload(restoreCtx)
	})
	require.NoError(t, soos.DaemonReload(context.Background()))
}

// unitResult returns systemd's Result property — "success", "timeout",
// "exit-code" — which is how the two failure modes below are told apart.
func unitResult(t *testing.T, unit string) string {
	t.Helper()
	props := unitProperties(t, unit)
	result, ok := props["Result"].(string)
	require.True(t, ok, "Result is %T, not a string", props["Result"])
	return result
}

// Test_ShaperDeviceWaitUnderSystemd_Integration proves what no property read can
// show: the start job really does block for the wait loop.
func Test_ShaperDeviceWaitUnderSystemd_Integration(t *testing.T) {
	requireRootForUnitTests(t)

	ctx := context.Background()

	t.Run("a sentinel that appears inside the budget is waited for", func(t *testing.T) {
		sentinel := filepath.Join(t.TempDir(), "device")
		installScratchUnit(t, waitProbeName, fmt.Sprintf(waitProbeUnit, 20, 30, sentinel))

		// Create it after the start job is already blocked in the wait loop.
		go func() {
			time.Sleep(3 * time.Second)
			_ = os.WriteFile(sentinel, nil, 0o644)
		}()

		start := time.Now()
		require.NoError(t, soos.StartService(ctx, waitProbeName),
			"the start job must block for the wait loop and then succeed, or a late device is never shaped")
		require.Greater(t, time.Since(start), 2*time.Second,
			"the start returned before the sentinel existed: the unit did not wait at all")
		require.Equal(t, "success", unitResult(t, waitProbeName))
	})

	// The negative control: without it the wait above proves nothing.
	t.Run("a sentinel that never appears fails once, on the device-missing status", func(t *testing.T) {
		sentinel := filepath.Join(t.TempDir(), "never")
		installScratchUnit(t, waitProbeName, fmt.Sprintf(waitProbeUnit, 3, 30, sentinel))

		require.Error(t, soos.StartService(ctx, waitProbeName))

		props := unitProperties(t, waitProbeName)
		require.Equal(t, "exit-code", props["Result"],
			"a device that never appeared must fail on its own exit status, not on a timeout")
		status, ok := props["ExecMainStatus"].(int32)
		require.True(t, ok, "ExecMainStatus is %T, not an int32", props["ExecMainStatus"])
		require.Equal(t, int32(shape.DeviceMissingExitCode), status,
			"the script must surface exit %d so `systemctl status` distinguishes a late device from a tc error",
			shape.DeviceMissingExitCode)

		// Long enough that a restart, were one scheduled, would have happened. The
		// unit ships no restart policy precisely so a bad config stays visible.
		time.Sleep(4 * time.Second)
		require.Equal(t, "failed", unitProperties(t, waitProbeName)["ActiveState"],
			"the unit must stay failed and visible, not quietly retry a config that cannot work")
	})

	// Why the shipped unit sets TimeoutStartSec= at all: on a Type=oneshot systemd
	// disables the start timeout, so a wait that outlives its budget hangs forever.
	t.Run("a wait that outlives the start timeout is killed by it", func(t *testing.T) {
		sentinel := filepath.Join(t.TempDir(), "never")
		installScratchUnit(t, waitProbeName, fmt.Sprintf(waitProbeUnit, 60, 3, sentinel))

		require.Error(t, soos.StartService(ctx, waitProbeName))
		require.Equal(t, "timeout", unitResult(t, waitProbeName),
			"TimeoutStartSec= must bound the wait loop; a Type=oneshot has no default timeout to fall back on")
	})
}
