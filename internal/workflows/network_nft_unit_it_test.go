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

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
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

// systemdAnalyzeCandidates are absolute paths only, never a bare
// "systemd-analyze" off PATH (see docs/dev/security-model.md).
var systemdAnalyzeCandidates = []string{"/usr/bin/systemd-analyze", "/bin/systemd-analyze"}

func requireRootForUnitTests(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}
}

// backupNetworkNftUnit snapshots the installed unit and its enablement and
// restores both afterwards, so a run on a real host changes nothing.
func backupNetworkNftUnit(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	path := firewall.NetworkNftServiceUnitPath

	original, readErr := os.ReadFile(path)
	existed := readErr == nil
	if !existed {
		require.True(t, os.IsNotExist(readErr), "could not read %s: %v", path, readErr)
	}
	wasEnabled, _ := soos.IsServiceEnabled(ctx, firewall.NetworkNftService)

	t.Cleanup(func() {
		restoreCtx := context.Background()
		if existed {
			_ = os.WriteFile(path, original, 0o644)
		} else {
			_ = os.Remove(path)
		}
		_ = soos.DaemonReload(restoreCtx)
		switch {
		case existed && wasEnabled:
			_ = soos.EnableService(restoreCtx, firewall.NetworkNftService)
		case !wasEnabled:
			_ = soos.DisableService(restoreCtx, firewall.NetworkNftService)
		}
	})
}

// unitOrdering returns the After= and Before= lists systemd itself parsed from
// the installed unit. Asking systemd is the point: a misspelled or misplaced
// directive is dropped here, while a string match on the file would still pass.
func unitOrdering(t *testing.T, unit string) (after, before []string) {
	t.Helper()
	ctx := context.Background()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Force systemd to load the unit; an unloaded unit exposes no properties.
	_, err = conn.ListUnitsByNamesContext(ctx, []string{unit})
	require.NoError(t, err)

	props, err := conn.GetUnitPropertiesContext(ctx, unit)
	require.NoError(t, err)

	return stringList(t, props, "After"), stringList(t, props, "Before")
}

func stringList(t *testing.T, props map[string]any, key string) []string {
	t.Helper()
	value, ok := props[key]
	require.True(t, ok, "unit property %q is absent", key)
	list, ok := value.([]string)
	require.True(t, ok, "unit property %q is %T, not a string list", key, value)
	return list
}

// Test_NetworkNftUnitOrdering_Integration pins the #982 boot ordering as systemd
// resolves it, not as the template spells it.
func Test_NetworkNftUnitOrdering_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupNetworkNftUnit(t)

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

// managerStubs mirror the ordering the real firewall managers declare, for the
// satisfiability check below. An absent unit resolves to a systemd stub with no
// dependencies, which nothing can cycle against — so without stubs the check
// would pass vacuously on exactly the hosts it is meant to cover (every CI VM).
//
// Only the [Unit] ordering is mirrored, since that is all a cycle can be made of.
// firewalld keeps default dependencies while the others disable them, as the real
// units do: that puts it after basic.target, and After= a unit that late plus
// Before=network-pre.target is the pair that would cycle if one were reachable
// from the other.
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

// verifyOrdering runs `systemd-analyze verify` over the given units and returns
// its output lowercased. The exit status is ignored: verify warns about unrelated
// things on a real host, so only the cycle diagnostic is read.
func verifyOrdering(t *testing.T, bin string, units ...string) string {
	t.Helper()
	out, _ := exec.CommandContext(context.Background(), bin,
		append([]string{"verify"}, units...)...).CombinedOutput()
	return strings.ToLower(string(out))
}

// Test_NetworkNftUnitIsSatisfiable_Integration checks the parsed ordering is
// actually reachable. An unsatisfiable one shows up as a cycle, and systemd
// breaks cycles by dropping a job — leaving the loader silently unstarted.
//
// Every unit must be named on the same command line: verify builds a start
// transaction for exactly the units it is given, and ordering pulls nothing else
// in, so verifying the loader alone can never see a cycle. The canary pins that,
// and the managers have to exist as units too (see managerStubs).
func Test_NetworkNftUnitIsSatisfiable_Integration(t *testing.T) {
	requireRootForUnitTests(t)
	backupNetworkNftUnit(t)
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
	backupNetworkNftUnit(t)

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

		needsConverge, err := firewall.NetworkNftUnitNeedsConverge(ctx)
		require.NoError(t, err)
		require.False(t, needsConverge)

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		require.False(t, applies, "the migration must not re-fire on a converged host")
	})

	// The failure a byte compare cannot see, against real systemd: the unit on
	// disk is the embedded copy and disabled. Before #982 the probe called this
	// "converged" and the host rebooted with no weaver tables.
	t.Run("a current but disabled unit is re-enabled", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, embedded, 0o644))
		require.NoError(t, soos.DaemonReload(ctx))
		require.NoError(t, soos.DisableService(ctx, firewall.NetworkNftService))

		enabled, err := soos.IsServiceEnabled(ctx, firewall.NetworkNftService)
		require.NoError(t, err)
		require.False(t, enabled, "precondition: systemd must be holding the unit disabled")

		needsConverge, err := firewall.NetworkNftUnitNeedsConverge(ctx)
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
