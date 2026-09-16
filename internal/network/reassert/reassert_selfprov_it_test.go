// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package reassert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// The tests in reassert_it_test.go all require a provisioned host — they skip
// unless `block node install` has run, which in practice means they rarely
// execute. These build their own fixtures instead: a .nft artifact under
// t.TempDir() and the real tables loaded from it, so the probe-and-repair loop
// is exercised against the live kernel on a bare host.
//
// What they cover: the real probes (firewall.Exists / policy.Exists via nft),
// the real flock, the Expected/Present/Reasserted/Recovered bookkeeping, and the
// re-probe after a repair.
//
// What they deliberately do NOT cover: policy.RestartNetworkNftService and
// shape.RestartTcEgressService. Those restart systemd units that only exist on a
// provisioned host, so the repair seam is replaced with a direct `nft -f` reload.
// A green run here is not evidence that the systemd repair path works — that
// remains the provisioned-host tests' job.

// skipIfProvisioned keeps these off a real node. There the artifacts are live
// state a test must not fabricate, and reassert_it_test.go already covers it.
func skipIfProvisioned(t *testing.T) {
	t.Helper()
	for _, path := range []string{firewall.HostNftPath, policy.WeaverNftPath} {
		if _, err := os.Stat(path); err == nil {
			t.Skipf("%s is provisioned here; the provisioned-host tests cover this", path)
		}
	}
}

// requireNftBinary resolves nft the same way production does, or skips.
func requireNftBinary(t *testing.T) string {
	t.Helper()
	bin, ok := nftexec.Binary()
	if !ok {
		t.Skipf("no nft binary on this host (looked for %s)", bin)
	}
	return bin
}

// scratchRuleset is a minimal but real definition of one weaver table. The
// chain matters: an empty table would load, but a table with a hook is what the
// production artifacts actually contain.
func scratchRuleset(table string) string {
	return "table " + table + " {\n" +
		"\tchain weaver_it_input {\n" +
		"\t\ttype filter hook input priority filter; policy accept;\n" +
		"\t}\n" +
		"}\n"
}

// writeScratchArtifacts renders both .nft artifacts into a temp dir and returns
// their paths. They carry the real table names because firewall.TableName and
// policy.TableName are constants the probes compile against.
func writeScratchArtifacts(t *testing.T) (hostPath, policyPath string) {
	t.Helper()
	dir := t.TempDir()

	hostPath = filepath.Join(dir, "host-firewall.nft")
	policyPath = filepath.Join(dir, "workload-policy.nft")

	require.NoError(t, os.WriteFile(hostPath, []byte(scratchRuleset("inet weaver-host-firewall")), 0o600))
	require.NoError(t, os.WriteFile(policyPath, []byte(scratchRuleset("inet weaver-workload-policy")), 0o600))
	return hostPath, policyPath
}

// loadScratchTables applies both artifacts and guarantees their removal, so a
// failed assertion never leaves weaver-named tables behind on the host.
func loadScratchTables(t *testing.T, nft, hostPath, policyPath string) {
	t.Helper()
	t.Cleanup(func() {
		for _, table := range []string{"weaver-host-firewall", "weaver-workload-policy"} {
			_ = exec.Command(nft, "delete", "table", "inet", table).Run()
		}
	})
	for _, path := range []string{hostPath, policyPath} {
		require.NoError(t, exec.Command(nft, "-f", path).Run(), "could not load %s", path)
	}
}

// scratchReasserter wires a Reasserter to the temp artifacts and to locks under
// t.TempDir(), leaving every probe on its production default. The two restart
// seams reload the artifacts directly, since there is no systemd unit here.
func scratchReasserter(t *testing.T, nft, hostPath, policyPath string) *Reasserter {
	t.Helper()
	lockDir := t.TempDir()

	return NewWithConfig(Config{
		HostNftPath:   hostPath,
		PolicyNftPath: policyPath,
		NftLockPath:   filepath.Join(lockDir, ".applying"),
		ShapeLockPath: filepath.Join(lockDir, ".tc-applying"),
		RestartNft: func(ctx context.Context) error {
			for _, path := range []string{hostPath, policyPath} {
				if err := exec.CommandContext(ctx, nft, "-f", path).Run(); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// Test_Reassert_SelfProvisioned_RestoresDeletedTables is the core loop on a bare
// host: both tables destroyed, both back after one pass, and the report says so.
func Test_Reassert_SelfProvisioned_RestoresDeletedTables_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	nft := requireNftBinary(t)

	hostPath, policyPath := writeScratchArtifacts(t)
	loadScratchTables(t, nft, hostPath, policyPath)
	require.True(t, nftTableExists(t, "inet", "weaver-host-firewall"))
	require.True(t, nftTableExists(t, "inet", "weaver-workload-policy"))

	deleteWeaverTables(t)
	require.False(t, nftTableExists(t, "inet", "weaver-host-firewall"))
	require.False(t, nftTableExists(t, "inet", "weaver-workload-policy"))

	report := scratchReasserter(t, nft, hostPath, policyPath).Reassert(context.Background())

	assert.True(t, nftTableExists(t, "inet", "weaver-host-firewall"),
		"host firewall table must be restored; report=%+v", report.Artifacts)
	assert.True(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"workload policy table must be restored; report=%+v", report.Artifacts)

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		got := statusOf(t, report, id)
		assert.True(t, got.Expected, "%s must be expected: its artifact is on disk (%s)", id, got.Detail)
		assert.True(t, got.Reasserted, "%s should be reported re-asserted (%s)", id, got.Detail)
		assert.True(t, got.Recovered, "%s should be reported recovered (%s)", id, got.Detail)
		assert.True(t, got.Present, "%s is live again, so it must not report present=false", id)
	}
	assert.Empty(t, report.Missing(), "nothing may be left missing after a successful pass")
	assert.Empty(t, report.Unhealthy(), "report=%+v", report.Artifacts)
}

// Test_Reassert_SelfProvisioned_HealthyHostIsANoOp pins the steady state against
// the live kernel: with both tables present nothing is restarted.
func Test_Reassert_SelfProvisioned_HealthyHostIsANoOp_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	nft := requireNftBinary(t)

	hostPath, policyPath := writeScratchArtifacts(t)
	loadScratchTables(t, nft, hostPath, policyPath)

	// A restart here would mean the probe read a live table as absent.
	r := NewWithConfig(Config{
		HostNftPath:   hostPath,
		PolicyNftPath: policyPath,
		NftLockPath:   filepath.Join(t.TempDir(), ".applying"),
		ShapeLockPath: filepath.Join(t.TempDir(), ".tc-applying"),
		RestartNft: func(context.Context) error {
			t.Error("a converged host must not restart the loader")
			return nil
		},
	})

	report := r.Reassert(context.Background())

	assert.Empty(t, report.Reasserted(), "report=%+v", report.Artifacts)
	assert.Empty(t, report.Unhealthy(), "report=%+v", report.Artifacts)
	assert.Empty(t, report.Missing(), "report=%+v", report.Artifacts)
}

// Test_Check_SelfProvisioned_NeverMutates pins --check against the live kernel:
// it must report the damage and leave it in place.
func Test_Check_SelfProvisioned_NeverMutates_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	nft := requireNftBinary(t)

	hostPath, policyPath := writeScratchArtifacts(t)
	loadScratchTables(t, nft, hostPath, policyPath)
	deleteWeaverTables(t)

	report := scratchReasserter(t, nft, hostPath, policyPath).Check(context.Background())

	assert.False(t, nftTableExists(t, "inet", "weaver-host-firewall"),
		"--check must not restore anything")
	assert.False(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"--check must not restore anything")

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		got := statusOf(t, report, id)
		assert.True(t, got.Expected, "%s (%s)", id, got.Detail)
		assert.False(t, got.Present, "%s (%s)", id, got.Detail)
		assert.False(t, got.Reasserted, "%s (%s)", id, got.Detail)
	}
	assert.Len(t, report.Missing(), 2, "both deleted tables must be reported missing")
}

// Test_Reassert_SelfProvisioned_OnlyTheMissingTableIsRepaired guards the
// asymmetric case against the live kernel: one table down, the healthy one
// still reports present and is not counted as repaired.
func Test_Reassert_SelfProvisioned_OnlyTheMissingTableIsRepaired_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	nft := requireNftBinary(t)

	hostPath, policyPath := writeScratchArtifacts(t)
	loadScratchTables(t, nft, hostPath, policyPath)

	require.NoError(t, exec.Command(nft, "delete", "table", "inet", "weaver-host-firewall").Run())
	require.True(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"precondition: only the host firewall table may be removed")

	report := scratchReasserter(t, nft, hostPath, policyPath).Reassert(context.Background())

	assert.True(t, nftTableExists(t, "inet", "weaver-host-firewall"),
		"the deleted table must be restored; report=%+v", report.Artifacts)

	host := statusOf(t, report, ArtifactHostFirewall)
	assert.True(t, host.Reasserted, "detail=%s", host.Detail)
	assert.True(t, host.Recovered, "detail=%s", host.Detail)

	// The loader restart rebuilds both tables, so the surviving one must still
	// report as it was found: present, and never repaired.
	pol := statusOf(t, report, ArtifactWorkloadPolicy)
	assert.True(t, pol.Present, "the surviving table must stay present; detail=%s", pol.Detail)
	assert.False(t, pol.Reasserted,
		"a table that was never missing must not be reported re-asserted; detail=%s", pol.Detail)
}

// Test_Reassert_SelfProvisioned_UnprovisionedArtifactIsNotExpected pins the
// gate: with no .nft file on disk the table is not weaver's business, so an
// absent table is neither expected nor repaired.
func Test_Reassert_SelfProvisioned_UnprovisionedArtifactIsNotExpected_Integration(t *testing.T) {
	requireRoot(t)
	skipIfProvisioned(t)
	dir := t.TempDir()

	r := NewWithConfig(Config{
		HostNftPath:   filepath.Join(dir, "absent-host.nft"),
		PolicyNftPath: filepath.Join(dir, "absent-policy.nft"),
		NftLockPath:   filepath.Join(dir, ".applying"),
		ShapeLockPath: filepath.Join(dir, ".tc-applying"),
		RestartNft: func(context.Context) error {
			t.Error("an unprovisioned host must never restart the loader")
			return nil
		},
	})

	report := r.Reassert(context.Background())

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		got := statusOf(t, report, id)
		assert.False(t, got.Expected, "%s must not be expected (%s)", id, got.Detail)
		assert.False(t, got.Reasserted, "%s must not be repaired (%s)", id, got.Detail)
	}
	assert.Empty(t, report.Missing(), "an unprovisioned host has nothing missing")
}

// Test_QdiscProbe_ShapedDeviceIsPresent is the positive half of the qdisc probe.
// The existing Test_QdiscProbe_* tests cover a missing device and an unshaped
// one; without this, "reports true when an HTB root is live" is only ever
// asserted through a fake.
func Test_QdiscProbe_ShapedDeviceIsPresent_Integration(t *testing.T) {
	requireRoot(t)
	dev := addDummyDevice(t)

	present, err := shape.QdiscRootExists(context.Background(), dev)
	require.NoError(t, err)
	require.False(t, present, "precondition: a fresh dummy device has no HTB root")

	require.NoError(t, exec.Command("/sbin/tc",
		"qdisc", "add", "dev", dev, "root", "handle", "1:", "htb", "default", "30").Run(),
		"could not build an HTB root on %s", dev)

	present, err = shape.QdiscRootExists(context.Background(), dev)

	require.NoError(t, err)
	assert.True(t, present, "a live HTB root must be reported present")
}

// addDummyDevice creates a throwaway dummy NIC and removes it on exit. It skips
// when the kernel has no dummy module rather than failing the run.
func addDummyDevice(t *testing.T) string {
	t.Helper()
	const dev = "wvit0"

	// A leftover from a killed run would make the fresh-device assertion lie.
	_ = exec.Command("/sbin/ip", "link", "del", dev).Run()

	if err := exec.Command("/sbin/ip", "link", "add", dev, "type", "dummy").Run(); err != nil {
		t.Skipf("cannot create a dummy device (is the dummy module available?): %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("/sbin/ip", "link", "del", dev).Run() })
	return dev
}
