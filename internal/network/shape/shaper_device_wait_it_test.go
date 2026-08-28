// SPDX-License-Identifier: Apache-2.0

//go:build integration

package shape

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Absolute paths only, never a bare binary name off PATH (see
// docs/dev/security-model.md).
var (
	ipBinCandidates = []string{"/usr/sbin/ip", "/sbin/ip", "/usr/bin/ip", "/bin/ip"}
	shBinCandidates = []string{"/bin/sh", "/usr/bin/sh"}
	tcBinCandidates = []string{"/usr/sbin/tc", "/sbin/tc", "/usr/bin/tc", "/bin/tc"}
)

// servicePath is systemd's default PATH for a service: the script calls `tc` by
// name, and the test runner's own PATH has no sbin dirs.
const servicePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// resolveBin returns the first candidate that exists, or skips the test.
func resolveBin(t *testing.T, what string, candidates []string) string {
	t.Helper()
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skipf("%s not found in %v", what, candidates)
	return ""
}

// newNetns creates a throwaway network namespace and deletes it afterwards, so
// the bond this test creates never reaches the host's device table.
func newNetns(t *testing.T, ipBin, name string) string {
	t.Helper()

	// A namespace left behind by a killed run would make `add` fail.
	_ = exec.CommandContext(context.Background(), ipBin, "netns", "delete", name).Run()

	out, err := exec.CommandContext(context.Background(), ipBin, "netns", "add", name).CombinedOutput()
	require.NoError(t, err, "could not create network namespace %s: %s", name, out)

	t.Cleanup(func() {
		_ = exec.CommandContext(context.Background(), ipBin, "netns", "delete", name).Run()
	})
	return name
}

// writeScript renders the boot replay for nic and writes it where the namespace
// can execute it.
func writeScript(t *testing.T, dir, nic string) string {
	t.Helper()
	rendered, err := renderTcEgressScript(nic)
	require.NoError(t, err)
	return writeRendered(t, dir, "solo-provisioner-bandwidth-shaper.sh", rendered)
}

// writeUnshapeScript renders the teardown replay for nic.
func writeUnshapeScript(t *testing.T, dir, nic string) string {
	t.Helper()
	rendered, err := renderTcEgressUnshapeScript(nic)
	require.NoError(t, err)
	return writeRendered(t, dir, "solo-provisioner-bandwidth-shaper-unshape.sh", rendered)
}

func writeRendered(t *testing.T, dir, name, rendered string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(rendered), 0o755))
	return path
}

// runScriptInNetns runs the rendered replay inside the namespace with the given
// device-wait budget, the way the systemd unit runs it at boot.
func runScriptInNetns(t *testing.T, ipBin, shBin, ns, script, waitSecs string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), ipBin, "netns", "exec", ns, shBin, script)
	// exec keeps the last value of a duplicate key, so these override the inherited ones.
	cmd.Env = append(os.Environ(), "PATH="+servicePath, DeviceWaitEnvVar+"="+waitSecs)
	return cmd
}

// addBond creates a bond inside the namespace — the device class from #980.
func addBond(t *testing.T, ipBin, ns, nic string) {
	t.Helper()
	out, err := exec.CommandContext(context.Background(),
		ipBin, "netns", "exec", ns, ipBin, "link", "add", nic, "type", "bond").CombinedOutput()
	require.NoError(t, err, "could not create bond %s in netns %s: %s", nic, ns, out)
}

// qdiscOn returns `tc qdisc show dev <nic>` from inside the namespace.
func qdiscOn(t *testing.T, ipBin, tcBin, ns, nic string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(),
		ipBin, "netns", "exec", ns, tcBin, "qdisc", "show", "dev", nic).CombinedOutput()
	require.NoError(t, err, "tc qdisc show dev %s failed in netns %s: %s", nic, ns, out)
	return string(out)
}

// Test_ShaperDeviceWait_Integration is #980 end to end: the replay shapes a bond that
// does not exist yet when it runs. The last subtest is the negative control.
func Test_ShaperDeviceWait_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ipBin := resolveBin(t, "ip", ipBinCandidates)
	shBin := resolveBin(t, "sh", shBinCandidates)
	tcBin := resolveBin(t, "tc", tcBinCandidates)

	const nic = "bond0"
	script := writeScript(t, t.TempDir(), nic)

	t.Run("a bond that appears late is still shaped", func(t *testing.T) {
		ns := newNetns(t, ipBin, "weaver-shape-late")

		cmd := runScriptInNetns(t, ipBin, shBin, ns, script, "30")
		require.NoError(t, cmd.Start())

		// Create the device after the replay started, as systemd-networkd does.
		time.Sleep(2 * time.Second)
		addBond(t, ipBin, ns, nic)

		require.NoError(t, cmd.Wait(), "the replay must wait for the device instead of failing")

		qdisc := qdiscOn(t, ipBin, tcBin, ns, nic)
		require.Contains(t, qdisc, "htb", "the bond must carry the HTB root after the replay")
	})

	t.Run("a bond that already exists is shaped immediately", func(t *testing.T) {
		ns := newNetns(t, ipBin, "weaver-shape-present")
		addBond(t, ipBin, ns, nic)

		out, err := runScriptInNetns(t, ipBin, shBin, ns, script, "30").CombinedOutput()
		require.NoError(t, err, "replay failed: %s", out)
		require.Contains(t, qdiscOn(t, ipBin, tcBin, ns, nic), "htb")
	})

	t.Run("without a wait budget the same late device is missed", func(t *testing.T) {
		ns := newNetns(t, ipBin, "weaver-shape-nowait")

		cmd := runScriptInNetns(t, ipBin, shBin, ns, script, "0")
		out, err := cmd.CombinedOutput()
		require.Error(t, err,
			"a replay with no wait budget must fail on a missing device — if this ever "+
				"succeeds, the wait proves nothing: %s", out)
		require.Contains(t, string(out), "does not exist",
			"the failure must name the missing device, not fall through to tc")
		// The diagnostic an operator reads off `systemctl status`; a tc failure
		// would surface tc's own status instead.
		require.Equal(t, DeviceMissingExitCode, cmd.ProcessState.ExitCode(),
			"a missing device must exit %d (EX_TEMPFAIL), not the status tc happens to return",
			DeviceMissingExitCode)

		// And the pre-#980 outcome: the device arrives, and nothing shaped it.
		addBond(t, ipBin, ns, nic)
		require.NotContains(t, qdiscOn(t, ipBin, tcBin, ns, nic), "htb")
	})
}

// Test_ShaperUnshapeWithoutDevice_Integration guards the other side of the wait:
// a teardown must succeed even with the NIC gone.
func Test_ShaperUnshapeWithoutDevice_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ipBin := resolveBin(t, "ip", ipBinCandidates)
	shBin := resolveBin(t, "sh", shBinCandidates)

	const nic = "bond0"
	script := writeUnshapeScript(t, t.TempDir(), nic)
	ns := newNetns(t, ipBin, "weaver-shape-unshape")

	// A generous budget: were the teardown to honour one, this would block then fail.
	out, err := runScriptInNetns(t, ipBin, shBin, ns, script, "30").CombinedOutput()
	require.NoError(t, err, "teardown must succeed with no device present: %s", out)
	require.NotContains(t, string(out), "does not exist")
}
