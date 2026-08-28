// SPDX-License-Identifier: Apache-2.0

//go:build integration

package shape

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// shaperReplayProbeUnit mirrors the shipped unit's [Service] block against a
// throwaway netns. Format args, in order: wait budget (s), PATH, start timeout
// (s), ip, netns, sh, script.
const shaperReplayProbeUnit = `[Unit]
Description=Bandwidth shaper boot replay probe (#980 verification)
DefaultDependencies=no
StartLimitIntervalSec=0

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=` + DeviceWaitEnvVar + `=%d
Environment=PATH=%s
TimeoutStartSec=%ds
ExecStart=%s netns exec %s %s %s
`

const shaperReplayProbeName = "solo-weaver-shaper-replay-probe.service"

// installScratchShaperUnit writes a throwaway unit under /etc/systemd/system and
// removes it afterwards. Stop before remove: it cancels a pending start job.
func installScratchShaperUnit(t *testing.T, name, content string) {
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

// Test_ShaperUnitReplaysLateDevice_Integration runs the rendered script under a
// real oneshot start job against a bond created after it started, then reads the
// HTB hierarchy back off the device. See docs/dev/traffic-shaper.md.
func Test_ShaperUnitReplaysLateDevice_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ipBin := resolveBin(t, "ip", ipBinCandidates)
	shBin := resolveBin(t, "sh", shBinCandidates)
	tcBin := resolveBin(t, "tc", tcBinCandidates)

	const (
		nic         = "bond0"
		waitSecs    = 20
		timeoutSecs = 40
		// Long enough that the start job is already blocked in the wait loop.
		appearAfter = 3 * time.Second
	)

	ns := newNetns(t, ipBin, "weaver-shape-unit")
	script := writeScript(t, t.TempDir(), nic)

	installScratchShaperUnit(t, shaperReplayProbeName, fmt.Sprintf(shaperReplayProbeUnit,
		waitSecs, servicePath, timeoutSecs, ipBin, ns, shBin, script))

	// Create the bond once the start job is waiting, as systemd-networkd does at
	// boot. FailNow is illegal off the test goroutine, so carry the outcome back.
	created := make(chan error, 1)
	go func() {
		time.Sleep(appearAfter)
		out, err := exec.CommandContext(context.Background(),
			ipBin, "netns", "exec", ns, ipBin, "link", "add", nic, "type", "bond").CombinedOutput()
		if err != nil {
			err = fmt.Errorf("could not create bond %s in netns %s: %s: %w", nic, ns, out, err)
		}
		created <- err
	}()

	start := time.Now()
	startErr := soos.StartService(context.Background(), shaperReplayProbeName)
	require.NoError(t, <-created)
	require.NoError(t, startErr,
		"the start job must wait for the bond and then replay the hierarchy")
	require.Greater(t, time.Since(start), appearAfter-time.Second,
		"the start job returned before the bond existed: it did not wait at all")

	require.Contains(t, qdiscOn(t, ipBin, tcBin, ns, nic), "htb",
		"the bond must carry the HTB root once the start job has completed")
}
