// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inClusterFwRunner is an in-memory firewall.Runner: no kernel, no systemd.
type inClusterFwRunner struct {
	exists   bool
	checkErr error
}

func (r *inClusterFwRunner) List(context.Context) (string, error) { return "", nil }
func (r *inClusterFwRunner) Delete(context.Context) error         { r.exists = false; return nil }
func (r *inClusterFwRunner) Exists(context.Context) (bool, error) { return r.exists, nil }
func (r *inClusterFwRunner) Check(context.Context, string) error  { return r.checkErr }

// inClusterHost is a host firewall wired to temp paths.
type inClusterHost struct {
	runner   *inClusterFwRunner
	applies  int
	fwConfig string
	nft      string
}

func newInClusterHost(t *testing.T) *inClusterHost {
	t.Helper()
	dir := t.TempDir()
	h := &inClusterHost{
		runner:   &inClusterFwRunner{},
		fwConfig: filepath.Join(dir, "network-weaver-host-firewall.yaml"),
		nft:      filepath.Join(dir, "network-weaver-host-firewall.nft"),
	}
	origMgr, origEuid := inClusterPortFirewallManager, inClusterPortGeteuid
	inClusterPortFirewallManager = h.manager
	inClusterPortGeteuid = func() int { return 0 }
	t.Cleanup(func() { inClusterPortFirewallManager, inClusterPortGeteuid = origMgr, origEuid })
	return h
}

func (h *inClusterHost) manager() *firewall.Manager {
	return firewall.NewManagerWithConfig(firewall.Config{
		Runner:     h.runner,
		NftPath:    h.nft,
		ConfigPath: h.fwConfig,
		LockPath:   filepath.Join(filepath.Dir(h.nft), ".applying"),
		// Stubbed: the production hook manages real systemd units.
		SyncRefreshTimer: func(context.Context, bool) error { return nil },
		ApplyViaService: func(context.Context) error {
			h.applies++
			h.runner.exists = true
			return nil
		},
	})
}

// createFirewall sets up a host firewall with the given in-cluster ports, the
// way an older build persisted it, and resets the apply counter.
func (h *inClusterHost) createFirewall(t *testing.T, ports ...string) {
	t.Helper()
	tbl := firewall.NewTable()
	tbl.Mgmt.CIDRs = []string{"192.168.50.0/24"}
	tbl.InCluster.CIDRs = []string{models.DefaultClusterPodCIDR}
	tbl.InCluster.Ports = ports
	_, err := h.manager().Create(context.Background(), tbl, false)
	require.NoError(t, err)
	h.applies = 0
}

func (h *inClusterHost) firewallPorts(t *testing.T) []string {
	t.Helper()
	tbl, err := h.manager().Table(context.Background())
	require.NoError(t, err)
	return tbl.InCluster.Ports
}

func runInClusterMigration(t *testing.T) {
	t.Helper()
	require.NoError(t, NewInClusterNodeExporterPortMigration().Execute(context.Background(), newMctx("0.36.1", inClusterNodeExporterPortMinVersion)))
}

var legacyPortSpecs = []string{"4244", "6443", "7472", "10250"}

func TestInClusterNodeExporterPortMigration_Applies(t *testing.T) {
	m := NewInClusterNodeExporterPortMigration()
	orig := inClusterPortGeteuid
	t.Cleanup(func() { inClusterPortGeteuid = orig })

	tests := []struct {
		name      string
		euid      int
		installed string
		current   string
		want      bool
	}{
		{"root crossing the boundary", 0, "0.36.1", inClusterNodeExporterPortMinVersion, true},
		{"not root", 1000, "0.36.1", inClusterNodeExporterPortMinVersion, false},
		{"already past the boundary", 0, inClusterNodeExporterPortMinVersion, "0.38.0", false},
		{"fresh install", 0, "", inClusterNodeExporterPortMinVersion, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inClusterPortGeteuid = func() int { return tc.euid }
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInClusterNodeExporterPortMigration_AddsPort(t *testing.T) {
	h := newInClusterHost(t)
	h.createFirewall(t, legacyPortSpecs...)

	state.TakeWarnings()
	runInClusterMigration(t)

	assert.Equal(t, []string{"4244", "6443", "7472", "9100", "10250"}, h.firewallPorts(t))
	requireOperatorWarning(t, false)
	nft, err := os.ReadFile(h.nft)
	require.NoError(t, err)
	assert.Contains(t, string(nft), "elements = { 4244, 6443, 7472, 9100, 10250 }")
	assert.Equal(t, 1, h.applies)

	t.Run("a second run changes nothing", func(t *testing.T) {
		runInClusterMigration(t)
		assert.Equal(t, 1, h.applies)
	})
}

func TestInClusterNodeExporterPortMigration_KeepsOperatorPorts(t *testing.T) {
	h := newInClusterHost(t)
	h.createFirewall(t, append([]string{"8080"}, legacyPortSpecs...)...)

	runInClusterMigration(t)

	assert.Equal(t, []string{"4244", "6443", "7472", "8080", "9100", "10250"}, h.firewallPorts(t))
}

func TestInClusterNodeExporterPortMigration_LeavesListAlone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ports []string
		// warns: only a narrowed list leaves the operator something to do.
		warns bool
	}{
		{"narrowed", []string{"6443", "10250"}, true},
		{"port inside a range", []string{"4244", "6443", "7472", "9000-9200", "10250"}, false},
		{"no ports", []string{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newInClusterHost(t)
			h.createFirewall(t, tc.ports...)
			state.TakeWarnings()

			runInClusterMigration(t)

			assert.Equal(t, tc.ports, h.firewallPorts(t))
			assert.Equal(t, 0, h.applies, "an untouched firewall must not be re-applied")
			requireOperatorWarning(t, tc.warns)
		})
	}
}

// requireOperatorWarning checks the warning the command's summary shows: the
// log reaches only the log file in the interactive mode.
func requireOperatorWarning(t *testing.T, want bool) {
	t.Helper()
	got := state.TakeWarnings()
	if !want {
		require.Empty(t, got)
		return
	}
	require.Len(t, got, 1)
	require.Contains(t, got[0], "network firewall add --in-cluster-port 9100")
}

func TestInClusterNodeExporterPortMigration_NoFirewall(t *testing.T) {
	h := newInClusterHost(t)

	runInClusterMigration(t)

	assert.NoFileExists(t, h.fwConfig)
	assert.NoFileExists(t, h.nft)
}

// A ruleset nft refuses leaves the firewall as it was, and the migration only
// warns, so the command the operator ran still runs.
func TestInClusterNodeExporterPortMigration_RefusedApplyChangesNothing(t *testing.T) {
	h := newInClusterHost(t)
	h.createFirewall(t, legacyPortSpecs...)
	h.runner.checkErr = errors.New("nft refused the ruleset")
	state.TakeWarnings()

	runInClusterMigration(t)

	assert.Equal(t, legacyPortSpecs, h.firewallPorts(t))
	assert.Equal(t, 0, h.applies)
	requireOperatorWarning(t, true)
}
