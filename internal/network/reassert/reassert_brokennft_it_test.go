// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package reassert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// The anti-thrash invariant against the real exec path. The unit tests drive
// nftexec with a fake binary behind a seam; here the fake stands in for the
// real binary, so the pre-gate, the runners and nftexec all run exactly as in
// production. Needs root and a provisioned host.

// shadowNftBinary bind-mounts a script that always fails over the nft binary the
// code under test resolves. The returned restore func unmounts it; it is also
// registered as a cleanup and is safe to call twice.
func shadowNftBinary(t *testing.T) (target string, restore func()) {
	t.Helper()
	target, ok := nftexec.Binary()
	require.True(t, ok, "nft binary not found; the probe cannot be exercised")

	fake := filepath.Join(t.TempDir(), "nft")
	require.NoError(t, os.WriteFile(fake,
		[]byte("#!/bin/sh\necho 'nft: simulated failure' >&2\nexit 1\n"), 0o755))

	require.NoError(t, syscall.Mount(fake, target, "", syscall.MS_BIND, ""),
		"bind-mounting the fake over %s", target)

	var once sync.Once
	restore = func() {
		once.Do(func() {
			if err := syscall.Unmount(target, 0); err != nil {
				t.Errorf("could not unmount the fake nft from %s: %v; run `umount %s` by hand",
					target, err, target)
			}
		})
	}
	t.Cleanup(restore)
	return target, restore
}

// unitInvocationID reads the unit's InvocationID, which systemd regenerates on
// every start. An unchanged value proves the unit was neither started nor
// restarted in between. Read via systemctl, independent of the code under test.
func unitInvocationID(t *testing.T, unit string) string {
	t.Helper()
	out, err := exec.Command("/usr/bin/systemctl", "show", "-p", "InvocationID", "--value", unit).Output()
	require.NoError(t, err, "systemctl show %s", unit)
	return strings.TrimSpace(string(out))
}

// reassertUnskipped runs Reassert, retrying briefly if the daemon's own tick
// happened to hold an apply lock, so a benign race cannot fail the test.
func reassertUnskipped(t *testing.T) Report {
	t.Helper()
	var report Report
	for attempt := 0; attempt < 3; attempt++ {
		report = New().Reassert(context.Background())
		if len(report.Skipped()) == 0 {
			return report
		}
		time.Sleep(2 * time.Second)
	}
	require.Empty(t, report.Skipped(), "an apply lock stayed held across three attempts")
	return report
}

// Test_Reassert_BrokenNftIsProbeFailureNotRepair: when nft itself cannot run,
// every provisioned table must read "unknown", nothing may be restarted, and
// the live tables must be left exactly as they were.
func Test_Reassert_BrokenNftIsProbeFailureNotRepair_Integration(t *testing.T) {
	requireRoot(t)
	// block node install always lays the policy plane down; the firewall is optional.
	requireArtifact(t, policy.WeaverNftPath)

	expected := []string{ArtifactWorkloadPolicy}
	tables := []string{"weaver-workload-policy"}
	if _, err := os.Stat(firewall.HostNftPath); err == nil {
		expected = append(expected, ArtifactHostFirewall)
		tables = append(tables, "weaver-host-firewall")
	}
	shaped := false
	if _, err := os.Stat(shape.TcEgressScriptPath); err == nil {
		_, shaped, err = shape.EgressNICFromScript()
		require.NoError(t, err)
	}

	// Preconditions, read while nft still works.
	for _, table := range tables {
		require.True(t, nftTableExists(t, "inet", table),
			"precondition: inet %s must be live before nft is shadowed", table)
	}
	before := unitInvocationID(t, policy.NetworkNftService)

	_, restore := shadowNftBinary(t)
	report := reassertUnskipped(t)
	after := unitInvocationID(t, policy.NetworkNftService)
	restore()

	for _, id := range expected {
		got := statusOf(t, report, id)
		assert.True(t, got.Expected, "%s is provisioned, so it must be expected", id)
		assert.True(t, got.ProbeFailed, "%s must be unknown when nft cannot run (%s)", id, got.Detail)
		assert.False(t, got.Reasserted, "%s must never be repaired on a probe failure (%s)", id, got.Detail)
		assert.False(t, got.Present, "%s cannot be reported present when nothing could look", id)
		assert.Contains(t, got.Detail, "cannot determine")
	}
	assert.Empty(t, report.Reasserted(), "nothing may be restarted when nft cannot answer")
	assert.Equal(t, before, after, "%s must not have been restarted", policy.NetworkNftService)

	// The tc plane does not depend on nft, so a broken nft must not taint it.
	if shaped {
		qdisc := statusOf(t, report, ArtifactEgressQdisc)
		assert.False(t, qdisc.ProbeFailed, "egress-qdisc must still be probed normally (%s)", qdisc.Detail)
		assert.True(t, qdisc.Present, "egress-qdisc was live and must still read present (%s)", qdisc.Detail)
	}

	// Nothing was touched: the tables are still live, and with nft back a plain
	// check reads them present again.
	for _, table := range tables {
		assert.True(t, nftTableExists(t, "inet", table), "inet %s must still be live", table)
	}
	check := New().Check(context.Background())
	for _, id := range expected {
		got := statusOf(t, check, id)
		assert.True(t, got.Present, "%s must read present once nft works again (%s)", id, got.Detail)
		assert.False(t, got.ProbeFailed, "%s must probe cleanly once nft works again (%s)", id, got.Detail)
	}
}
