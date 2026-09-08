// SPDX-License-Identifier: Apache-2.0

//go:build integration

package workflows

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// networkNftUnitTemplate mirrors the private const of the same name in
// internal/network/firewall; the tests compare the installed unit against it.
const networkNftUnitTemplate = "files/network/solo-provisioner-network-nft.service"

// staleNetworkNftUnit is the pre-#982 unit: no ordering against the firewall
// managers, no network-pre.target. This is what an upgraded host has on disk.
const staleNetworkNftUnit = `[Unit]
Description=Solo Provisioner Network Rules (nftables)
DefaultDependencies=no
After=local-fs.target
Before=solo-provisioner-daemon.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`

// Test_NetworkNftUnitOrdering_Integration pins the #982 boot ordering as systemd
// resolves it, not as the template spells it.
func Test_NetworkNftUnitOrdering_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupUnit(t, firewall.NetworkNftServiceUnitPath, firewall.NetworkNftService)

	require.NoError(t, firewall.EnsureNetworkNftUnit(context.Background()))

	after, before := unitOrdering(t, firewall.NetworkNftService)

	// The managers are named even when not installed: systemd records the ordering
	// against a stub, so After= is inert now and takes effect if one is installed.
	for _, dep := range []string{
		"local-fs.target",
		"nftables.service",
		"ufw.service",
		"firewalld.service",
	} {
		require.Contains(t, after, dep, "the loader must be ordered after %s", dep)
	}

	require.Contains(t, before, "network-pre.target",
		"the rules must be in place before any interface comes up")
	require.Contains(t, before, "solo-provisioner-daemon.service")

	// Guard against the inversion: going before a manager that flushes the ruleset
	// is what wipes the weaver tables on every boot.
	require.NotContains(t, before, "nftables.service")
	require.NotContains(t, before, "ufw.service")
	require.NotContains(t, before, "firewalld.service")
	require.NotContains(t, after, "network-pre.target")
}

// firewallManagerUnits fixes the order the stubs are installed and named in,
// since map iteration is not stable.
var firewallManagerUnits = []string{"nftables.service", "ufw.service", "firewalld.service"}

// managerStubs mirror the [Unit] ordering the real firewall managers declare,
// for the satisfiability check below. Without them an absent manager resolves to
// a dependency-free systemd stub, and the check passes vacuously on exactly the
// hosts it covers (every CI VM). firewalld keeps default dependencies as the real
// unit does, since that is what puts it late enough to cycle.
var managerStubs = map[string]string{
	"nftables.service": `[Unit]
Description=Stub nftables.service (#982 ordering verification)
DefaultDependencies=no
Wants=network-pre.target
Before=network-pre.target shutdown.target
Conflicts=shutdown.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true

[Install]
WantedBy=sysinit.target
`,
	"ufw.service": `[Unit]
Description=Stub ufw.service (#982 ordering verification)
DefaultDependencies=no
Wants=network-pre.target
Before=network-pre.target network.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`,
	"firewalld.service": `[Unit]
Description=Stub firewalld.service (#982 ordering verification)
Wants=network-pre.target
Before=network-pre.target
After=dbus.service polkit.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/true

[Install]
WantedBy=multi-user.target
`,
}

// installManagerStubs drops a stub in for every manager this host lacks and
// removes it afterwards. A really installed manager is left alone, since its own
// ordering is what the check wants to run against.
func installManagerStubs(t *testing.T) {
	t.Helper()

	for _, unit := range firewallManagerUnits {
		content, ok := managerStubs[unit]
		require.True(t, ok, "no stub defined for %s", unit)
		if systemdKnowsUnit(t, unit) {
			continue
		}
		path := filepath.Join("/etc/systemd/system", unit)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		t.Cleanup(func() {
			_ = os.Remove(path)
			_ = soos.DaemonReload(context.Background())
		})
	}
	require.NoError(t, soos.DaemonReload(context.Background()))

	// Without this, a stub that failed to land would make the check vacuous again
	// instead of failing.
	for _, unit := range firewallManagerUnits {
		require.True(t, systemdKnowsUnit(t, unit),
			"systemd must resolve %s, or the cycle check proves nothing", unit)
	}
}

// cycleCanaryUnit is the negative control: it orders itself both after and
// before the loader, which nothing can satisfy. If `systemd-analyze verify`
// reports no cycle for this, it is not checking cross-unit ordering at all.
const cycleCanaryUnit = `[Unit]
Description=Ordering cycle canary (#982 verification control)
DefaultDependencies=no
After=` + firewall.NetworkNftService + `
Before=` + firewall.NetworkNftService + `

[Service]
Type=oneshot
ExecStart=/bin/true
`

const cycleCanaryName = "solo-weaver-cycle-canary.service"

// installCycleCanary drops the canary in and removes it afterwards. It is never
// named on the real check's command line, and verify only orders the units it is
// given, so it cannot make that check fail.
func installCycleCanary(t *testing.T) {
	t.Helper()

	path := filepath.Join("/etc/systemd/system", cycleCanaryName)
	require.NoError(t, os.WriteFile(path, []byte(cycleCanaryUnit), 0o644))
	require.NoError(t, soos.DaemonReload(context.Background()))

	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = soos.DaemonReload(context.Background())
	})
}

// Test_NetworkNftUnitIsSatisfiable_Integration checks the parsed ordering is
// reachable: systemd breaks a cycle by dropping a job, leaving the loader
// silently unstarted. Every unit must be named on the same command line, since
// verify only orders the units it is given — the canary pins that.
func Test_NetworkNftUnitIsSatisfiable_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupUnit(t, firewall.NetworkNftServiceUnitPath, firewall.NetworkNftService)
	installManagerStubs(t)
	installCycleCanary(t)

	require.NoError(t, firewall.EnsureNetworkNftUnit(context.Background()))

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

	units := append([]string{firewall.NetworkNftServiceUnitPath}, firewallManagerUnits...)

	control := verifyOrdering(t, bin, append(units, cycleCanaryName)...)
	require.Contains(t, control, "ordering cycle",
		"control: an unsatisfiable ordering must be reported, or this invocation "+
			"cannot detect one and the assertion below is vacuous:\n%s", control)

	out := verifyOrdering(t, bin, units...)
	require.NotContains(t, out, "ordering cycle",
		"systemd-analyze verify reported an ordering cycle:\n%s", out)
}

// Test_NetworkNftUnitMigration_Integration exercises the #982 delivery path
// against real systemd: an upgraded host has a stale unit and runs no mutation,
// so the startup migration is the only thing that can converge it.
func Test_NetworkNftUnitMigration_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupUnit(t, firewall.NetworkNftServiceUnitPath, firewall.NetworkNftService)

	ctx := context.Background()
	path := firewall.NetworkNftServiceUnitPath
	embedded, err := templates.Files.ReadFile(networkNftUnitTemplate)
	require.NoError(t, err)

	m := NewNetworkNftUnitMigration()
	mctx := newMctx("0.28.1", "0.29.0")

	t.Run("a stale unit is rewritten, reloaded and enabled", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte(staleNetworkNftUnit), 0o644))
		require.NoError(t, soos.DaemonReload(ctx))

		// Precondition: systemd holds the stale ordering, so the assertions after
		// Execute cannot be met by an earlier test's write.
		after, _ := unitOrdering(t, firewall.NetworkNftService)
		require.NotContains(t, after, "nftables.service")

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.True(t, applies, "a stale unit must be detected as drift")

		require.NoError(t, m.Execute(ctx, mctx))

		current, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(embedded), string(current),
			"the converged unit must be the embedded copy byte-for-byte")

		// Read back through systemd: this proves Execute daemon-reloaded, not just
		// wrote bytes systemd never picked up.
		after, before := unitOrdering(t, firewall.NetworkNftService)
		require.Contains(t, after, "nftables.service")
		require.Contains(t, before, "network-pre.target")

		enabled, err := soos.IsServiceEnabled(ctx, firewall.NetworkNftService)
		require.NoError(t, err)
		require.True(t, enabled, "the converged unit must be enabled, or it never runs at boot")
	})

	t.Run("the converged host is left alone on the next invocation", func(t *testing.T) {
		// Set the precondition here instead of inheriting it, so a -run filter on
		// this name cannot pass vacuously.
		require.NoError(t, os.WriteFile(path, embedded, 0o644))
		require.NoError(t, soos.DaemonReload(ctx))
		require.NoError(t, soos.EnableService(ctx, firewall.NetworkNftService))

		needsConverge, err := firewall.NetworkNftUnitNeedsConverge()
		require.NoError(t, err)
		require.False(t, needsConverge)

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.False(t, applies, "the migration must not re-fire on a converged host")
	})

	// The failure a byte compare cannot see: the embedded copy on disk, disabled.
	// Before #982 the probe called that converged and the tables never came back.
	t.Run("a current but disabled unit is re-enabled", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, embedded, 0o644))
		require.NoError(t, soos.DaemonReload(ctx))
		require.NoError(t, soos.DisableService(ctx, firewall.NetworkNftService))

		enabled, err := soos.IsServiceEnabled(ctx, firewall.NetworkNftService)
		require.NoError(t, err)
		require.False(t, enabled, "precondition: systemd must be holding the unit disabled")

		needsConverge, err := firewall.NetworkNftUnitNeedsConverge()
		require.NoError(t, err)
		require.True(t, needsConverge, "a byte-current unit that will not run at boot is drift")

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.True(t, applies)

		require.NoError(t, m.Execute(ctx, mctx))

		enabled, err = soos.IsServiceEnabled(ctx, firewall.NetworkNftService)
		require.NoError(t, err)
		require.True(t, enabled, "Execute must enable a unit it did not need to rewrite")

		// The bytes were already current, so nothing may have been rewritten.
		current, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(embedded), string(current))
	})

	t.Run("an unprovisioned host gets no boot unit", func(t *testing.T) {
		for _, artifact := range []string{firewall.HostNftPath, firewall.WeaverNftPath} {
			if _, err := os.Stat(artifact); err == nil {
				t.Skipf("host has a persisted nft artifact (%s); it is provisioned", artifact)
			}
		}
		// RemoveAll, not Remove: under a -run filter the unit was never written,
		// and an ENOENT would fail the test before it asserts.
		require.NoError(t, os.RemoveAll(path))
		require.NoError(t, soos.DaemonReload(ctx))

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.False(t, applies,
			"a host with no unit and no persisted artifact must not get a boot unit installed")
		require.NoFileExists(t, path)
	})
}

// Test_NetworkNftUnitPartOfPropagation_Integration proves that restarting a
// firewall manager makes systemd re-run the loader immediately, asserting on
// what systemd DID rather than the unit file text, which a rejected directive
// would match just as happily.
func Test_NetworkNftUnitPartOfPropagation_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	ctx := context.Background()

	// Read before installManagerStubs, which leaves a really installed manager
	// alone. A real manager is the wrong lever: nftables.service flushes the host
	// ruleset when it cycles, so the propagation is exercised through a stub.
	absent := absentManagerUnits(t)

	backupUnit(t, firewall.NetworkNftServiceUnitPath, firewall.NetworkNftService)
	installManagerStubs(t)
	require.NoError(t, firewall.EnsureNetworkNftUnit(ctx))

	// systemd parsed the directive, against all three managers.
	partOf := stringList(t, unitProperties(t, firewall.NetworkNftService), "PartOf")
	for _, unit := range firewallManagerUnits {
		require.Contains(t, partOf, unit, "the loader must be PartOf %s", unit)
	}

	if len(absent) == 0 {
		t.Skip("every firewall manager is really installed here; cycling one would flush the host ruleset")
	}
	lever := absent[0]

	require.NoError(t, soos.StartService(ctx, lever))
	t.Cleanup(func() { _ = soos.StopService(context.Background(), lever) })
	require.NoError(t, soos.StartService(ctx, firewall.NetworkNftService))
	t.Cleanup(func() { _ = soos.StopService(context.Background(), firewall.NetworkNftService) })

	before := activeEnterTimestamp(t, firewall.NetworkNftService)
	require.NotZero(t, before, "the loader must be active before the propagation is observable")

	require.NoError(t, soos.RestartService(ctx, lever))

	// The propagated job is queued, not synchronous with the restart call.
	require.Eventually(t, func() bool {
		return activeEnterTimestamp(t, firewall.NetworkNftService) > before
	}, 15*time.Second, 200*time.Millisecond,
		"restarting %s must propagate a restart to the loader", lever)

	running, err := soos.IsServiceRunning(ctx, firewall.NetworkNftService)
	require.NoError(t, err)
	require.True(t, running, "the loader must be active again after the propagated restart")
}

// absentManagerUnits names the firewall managers this host does not really have,
// read BEFORE the stubs go in. A stub cycles exactly as the real unit would as
// far as propagation is concerned, but has no ExecStop of its own to flush the
// host's live ruleset — so it is the only safe lever for these tests.
func absentManagerUnits(t *testing.T) []string {
	t.Helper()

	var out []string
	for _, unit := range firewallManagerUnits {
		if !systemdKnowsUnit(t, unit) {
			out = append(out, unit)
		}
	}
	return out
}

// activeEnterTimestamp reads when the unit last became active. A oneshot with
// RemainAfterExit stays active, so a moving value proves it was cycled.
func activeEnterTimestamp(t *testing.T, unit string) uint64 {
	t.Helper()
	value, ok := unitProperties(t, unit)["ActiveEnterTimestampMonotonic"]
	require.True(t, ok, "unit property ActiveEnterTimestampMonotonic is absent")
	ts, ok := value.(uint64)
	require.True(t, ok, "ActiveEnterTimestampMonotonic is %T, not uint64", value)
	return ts
}

// itProbeTable is a throwaway nft table this suite owns. It is loaded through
// the loader unit's own ExecStart, so it stands in for a real weaver table
// without the test ever touching one.
const itProbeTable = "inet weaver-it-partof-probe"

// seedLoaderArtifact writes a scoped table to the loader's own artifact path so
// the unit's real ExecStart puts it in the kernel, and removes it afterwards.
// A host that already has the artifact is skipped rather than clobbered: that
// file is a provisioned host's live host firewall.
func seedLoaderArtifact(t *testing.T, nftBin string) {
	t.Helper()

	if _, err := os.Stat(firewall.HostNftPath); err == nil {
		t.Skipf("%s already exists; refusing to overwrite a provisioned host firewall", firewall.HostNftPath)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(firewall.HostNftPath), 0o755))
	require.NoError(t, os.WriteFile(firewall.HostNftPath,
		[]byte("table "+itProbeTable+" {\n}\n"), 0o600))

	t.Cleanup(func() {
		_ = os.Remove(firewall.HostNftPath)
		del := append([]string{"delete", "table"}, strings.Fields(itProbeTable)...)
		_ = exec.CommandContext(context.Background(), nftBin, del...).Run()
	})
}

// probeTableLive reads the live ruleset rather than reusing any weaver code, so
// the assertion is independent of the packages under test.
func probeTableLive(t *testing.T, nftBin string) bool {
	t.Helper()

	out, err := exec.CommandContext(context.Background(), nftBin, "list", "tables").Output()
	require.NoError(t, err)
	return nftexec.TableListed(string(out), itProbeTable)
}

// Test_NetworkNftUnitPartOfStopIsInert_Integration is the other half of the
// PartOf change. Propagation is bidirectional: `systemctl stop <manager>` now
// stops the loader too, and that must destroy nothing. A future ExecStop on this
// oneshot would silently turn every manager stop into a weaver-table teardown.
func Test_NetworkNftUnitPartOfStopIsInert_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	ctx := context.Background()

	// Read before installManagerStubs, for the reason absentManagerUnits gives.
	absent := absentManagerUnits(t)

	backupUnit(t, firewall.NetworkNftServiceUnitPath, firewall.NetworkNftService)
	installManagerStubs(t)
	require.NoError(t, firewall.EnsureNetworkNftUnit(ctx))

	// systemd parsed no ExecStop, so a propagated stop runs no teardown command.
	// This is the invariant the production comment claims; assert it on what
	// systemd loaded, not on the template text.
	props := unitProperties(t, firewall.NetworkNftService)
	stop, ok := props["ExecStop"]
	require.True(t, ok, "unit property ExecStop is absent")
	require.Empty(t, stop,
		"the loader must have no ExecStop, or a manager stop would tear the weaver tables down")

	if len(absent) == 0 {
		t.Skip("every firewall manager is really installed here; cycling one would flush the host ruleset")
	}
	lever := absent[0]

	nftBin, found := nftexec.Binary()
	if !found {
		t.Skipf("nft not found (looked for %s)", nftBin)
	}

	// Seeded before the loader starts, so its ExecStart is what loads the table.
	seedLoaderArtifact(t, nftBin)

	require.NoError(t, soos.StartService(ctx, lever))
	t.Cleanup(func() { _ = soos.StopService(context.Background(), lever) })
	require.NoError(t, soos.StartService(ctx, firewall.NetworkNftService))
	t.Cleanup(func() { _ = soos.StopService(context.Background(), firewall.NetworkNftService) })

	require.True(t, probeTableLive(t, nftBin),
		"precondition: the loader must have put %s in the kernel", itProbeTable)

	require.NoError(t, soos.StopService(ctx, lever))

	// The propagated job is queued, not synchronous with the stop call.
	require.Eventually(t, func() bool {
		running, err := soos.IsServiceRunning(ctx, firewall.NetworkNftService)
		return err == nil && !running
	}, 15*time.Second, 200*time.Millisecond,
		"stopping %s must propagate a stop to the loader", lever)

	// The whole point: the loader went down and took nothing with it.
	require.True(t, probeTableLive(t, nftBin),
		"a propagated stop must not remove a table the loader had put in the kernel")

	// And a manager start brings the loader back up, so the host does not sit
	// unprotected until the next boot.
	require.NoError(t, soos.StartService(ctx, lever))
	require.NoError(t, soos.StartService(ctx, firewall.NetworkNftService))
	running, err := soos.IsServiceRunning(ctx, firewall.NetworkNftService)
	require.NoError(t, err)
	require.True(t, running)
	require.True(t, probeTableLive(t, nftBin), "the replayed table must be live again")
}
