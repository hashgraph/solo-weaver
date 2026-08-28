// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/stretchr/testify/require"
)

// readShaperUnit returns the embedded unit file as a string.
func readShaperUnit(t *testing.T) string {
	t.Helper()
	content, err := templates.Files.ReadFile(tcEgressServiceTemplate)
	require.NoError(t, err)
	return string(content)
}

// unitDirectives returns the unit's directive lines grouped by [Section], with
// comments and blanks dropped: the unit's own prose names the directives asserted
// on below, and systemd ignores one that lands in the wrong section.
func unitDirectives(unit string) map[string][]string {
	sections := map[string][]string{}
	section := ""
	for _, raw := range strings.Split(unit, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		sections[section] = append(sections[section], line)
	}
	return sections
}

// requireDirective asserts the unit sets directive verbatim in section. Matching a
// whole line, not a substring, is what keeps a commented-out directive from
// satisfying the assertion.
func requireDirective(t *testing.T, unit, section, directive string, msgAndArgs ...any) {
	t.Helper()
	require.Contains(t, unitDirectives(unit)[section], directive, msgAndArgs...)
}

// requireNoDirective asserts no section sets key, whatever value it is given. The
// key is matched exactly, so "Restart" does not answer for "RestartSec".
func requireNoDirective(t *testing.T, unit, key string, msgAndArgs ...any) {
	t.Helper()
	for section, directives := range unitDirectives(unit) {
		for _, directive := range directives {
			set, _, found := strings.Cut(directive, "=")
			require.False(t, found && set == key,
				"[%s] must not set %s, but has %q: %v", section, key, directive, msgAndArgs)
		}
	}
}

// directiveValue returns the value the unit assigns key in section.
func directiveValue(t *testing.T, unit, section, key string) string {
	t.Helper()
	for _, directive := range unitDirectives(unit)[section] {
		if set, value, found := strings.Cut(directive, "="); found && set == key {
			return value
		}
	}
	require.Failf(t, "directive absent", "[%s] sets no %s=", section, key)
	return ""
}

// environmentValue returns the value the unit's [Service] Environment= lines assign
// to key.
func environmentValue(t *testing.T, unit, key string) (string, bool) {
	t.Helper()
	prefix := "Environment=" + key + "="
	for _, directive := range unitDirectives(unit)["Service"] {
		if strings.HasPrefix(directive, prefix) {
			return strings.TrimPrefix(directive, prefix), true
		}
	}
	return "", false
}

// TestTcEgressUnit_OrderingDirectives pins the #980 boot order: the replay runs
// once the links are up.
func TestTcEgressUnit_OrderingDirectives(t *testing.T) {
	unit := readShaperUnit(t)

	requireDirective(t, unit, "Unit", "Wants=network-online.target",
		"After= alone is inert unless something pulls network-online.target into the transaction")
	requireDirective(t, unit, "Unit", "After=network-online.target")
}

// TestTcEgressUnit_RetryDirectives pins the retry half of #980: a device that is
// late must not leave the NIC unshaped for the whole boot.
func TestTcEgressUnit_RetryDirectives(t *testing.T) {
	unit := readShaperUnit(t)

	requireDirective(t, unit, "Service", "Type=oneshot")
	requireDirective(t, unit, "Service", "RemainAfterExit=yes",
		"active (exited) is the operator's 'shaping applied' signal")

	// The retry is the script's wait loop, not RestartForceExitStatus= — see
	// docs/dev/traffic-shaper.md for why.
	requireNoDirective(t, unit, "RestartForceExitStatus",
		"RestartForceExitStatus= is a no-op for Type=oneshot on systemd < 256 (systemd#31148); "+
			"the supported hosts ship 249 and 252, so the retry cannot rest on it")
	// Restart= does work on a oneshot there, but only unscoped: it would delete and
	// rebuild the root qdisc every RestartSec forever on a config tc keeps rejecting.
	requireNoDirective(t, unit, "Restart",
		"a blanket Restart= would retry a permanently bad tc config for the uptime of the host")
	requireNoDirective(t, unit, "RestartSec")

	// Nothing restarts the unit automatically, so the only starts the limit could
	// ever count are an operator's `network shape` commands.
	requireDirective(t, unit, "Unit", "StartLimitIntervalSec=0",
		"with no restart policy a start limit can only refuse an operator command")
	requireNoDirective(t, unit, "StartLimitBurst")

	// No script on disk means nothing to replay: stay inactive, do not fail.
	requireDirective(t, unit, "Unit", "ConditionPathExists="+TcEgressScriptPath)
	requireDirective(t, unit, "Service", "ExecStart="+TcEgressScriptPath)

	// Assert the value, not just the key: a budget of 0 ships a wait loop that
	// never waits, and every other assertion here would still pass.
	budget := shippedWaitBudget(t, unit)
	require.Positive(t, budget, "the unit must ship a usable device-wait budget")
	// Type=oneshot disables the start timeout by default, so an explicit one is the
	// only bound on a hung wait — and it has to outlast the wait itself.
	timeout := shippedStartTimeoutSecs(t, unit)
	require.Greater(t, timeout, budget,
		"TimeoutStartSec= must outlast the device-wait budget, or systemd kills the wait it granted")
	// The budget is a synchronous cost on both paths: it delays multi-user.target at
	// boot and blocks every `network shape` mutation through ApplyTcEgressScript.
	require.LessOrEqual(t, budget, maxWaitBudgetSecs,
		"a budget this large is charged to every boot and every mutation on a host whose device never appears")
}

// maxWaitBudgetSecs caps the wait: it is paid synchronously by the boot transaction
// and by every `network shape` command, not in the background.
const maxWaitBudgetSecs = 45

// shippedStartTimeoutSecs returns the unit's TimeoutStartSec= in seconds. Only
// `<n>s` is accepted, so the comparison above cannot be fooled by "1min".
func shippedStartTimeoutSecs(t *testing.T, unit string) int {
	t.Helper()
	value := directiveValue(t, unit, "Service", "TimeoutStartSec")
	require.Regexp(t, `^\d+s$`, value,
		"the unit must set TimeoutStartSec= to a whole number of seconds")
	secs, err := strconv.Atoi(strings.TrimSuffix(value, "s"))
	require.NoError(t, err)
	return secs
}

// shippedWaitBudget returns the seconds the unit gives the script to wait.
func shippedWaitBudget(t *testing.T, unit string) int {
	t.Helper()
	value, ok := environmentValue(t, unit, DeviceWaitEnvVar)
	require.True(t, ok, "the unit must export %s to the script", DeviceWaitEnvVar)
	require.Regexp(t, regexp.MustCompile(`^\d+$`), value,
		"the unit must set %s to an integer number of seconds", DeviceWaitEnvVar)
	budget, err := strconv.Atoi(value)
	require.NoError(t, err)
	return budget
}

// TestTcEgressUnit_WaitBudgetReachesTheScript pins both halves to one variable: the
// unit exports it, the rendered script reads it with a fail-fast default.
func TestTcEgressUnit_WaitBudgetReachesTheScript(t *testing.T) {
	_, ok := environmentValue(t, readShaperUnit(t), DeviceWaitEnvVar)
	require.True(t, ok, "the unit must export %s from its [Service] section", DeviceWaitEnvVar)

	rendered, err := renderTcEgressScript("bond0")
	require.NoError(t, err)
	require.Contains(t, rendered, `WAIT_SECS="${`+DeviceWaitEnvVar+`:-0}"`,
		"the script must read the same variable the unit exports, defaulting to fail-fast")
}

// TestTcEgressUnit_DeviceMissingExitIsDistinct keeps the diagnostic status the
// operator sees in `systemctl status` apart from a generic tc failure.
func TestTcEgressUnit_DeviceMissingExitIsDistinct(t *testing.T) {
	rendered, err := renderTcEgressScript("bond0")
	require.NoError(t, err)

	require.Contains(t, rendered, "exit "+strconv.Itoa(DeviceMissingExitCode),
		"a device that never appeared must exit %d (EX_TEMPFAIL), not the status tc happens to return",
		DeviceMissingExitCode)
}

// TestRenderTcEgressScript_WaitsForVirtualDevice covers the AC: for a netplan-created
// bond, bridge or VLAN the replay waits instead of failing on "Cannot find device".
func TestRenderTcEgressScript_WaitsForVirtualDevice(t *testing.T) {
	for _, nic := range []string{"bond0", "br0", "vlan100", "enp0s1"} {
		t.Run(nic, func(t *testing.T) {
			rendered, err := renderTcEgressScript(nic)
			require.NoError(t, err)

			require.Contains(t, rendered, `WAIT_SECS="${`+DeviceWaitEnvVar+`:-0}"`,
				"the wait budget must come from the unit, defaulting to fail-fast")
			require.Contains(t, rendered, `while [ ! -e /sys/class/net/"$NIC" ]; do`)
			// A wait after the first tc call would guard nothing.
			require.Less(t, strings.Index(rendered, "/sys/class/net"), strings.Index(rendered, "tc qdisc"),
				"the device wait must run before the first tc call")
		})
	}
}

// TestRenderTcEgressUnshapeScript_DoesNotWaitForDevice keeps teardown working on a
// host whose NIC is gone: waiting there would turn it into a permanent retry loop.
func TestRenderTcEgressUnshapeScript_DoesNotWaitForDevice(t *testing.T) {
	rendered, err := renderTcEgressUnshapeScript("bond0")
	require.NoError(t, err)

	require.NotContains(t, rendered, DeviceWaitEnvVar)
	require.NotContains(t, rendered, "while [ ! -e")
	require.Contains(t, rendered, `tc qdisc del dev "$NIC" root 2>/dev/null || true`)
}
