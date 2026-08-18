// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeRunner satisfies firewall.Runner without touching the kernel. Only
// Exists is exercised here — the seed is a pure presence probe, and the content
// tier reads the persisted config rather than the kernel.
type probeRunner struct{ exists bool }

func (p *probeRunner) List(context.Context) (string, error) { return "", nil }
func (p *probeRunner) Check(context.Context, string) error  { return nil }
func (p *probeRunner) Delete(context.Context) error         { p.exists = false; return nil }
func (p *probeRunner) Exists(context.Context) (bool, error) { return p.exists, nil }

// stubFirewallManager points the package's firewall seam at a manager backed by
// a fake nft runner and temp artifact paths. active drives the IsActive probe;
// table, when non-nil, is written to the config path so Manager.Table loads it.
func stubFirewallManager(t *testing.T, active bool, table *firewall.Table) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "network-weaver-host-firewall.yaml")

	if table != nil {
		data, err := firewall.FileConfigFromTable(table).Marshal()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(configPath, data, 0o600))
	}

	orig := newHostFirewallManager
	newHostFirewallManager = func() *firewall.Manager {
		return firewall.NewManagerWithConfig(firewall.Config{
			Runner:          &probeRunner{exists: active},
			NftPath:         filepath.Join(dir, "network-weaver-host-firewall.nft"),
			ConfigPath:      configPath,
			LockPath:        filepath.Join(dir, ".applying"),
			ApplyViaService: func(context.Context) error { return nil },
		})
	}
	t.Cleanup(func() { newHostFirewallManager = orig })
}

// TestApplyPersistedFirewallContent_ConfigWins verifies the config > state
// precedence (issue #932, AC4): fields the operator supplied via --config are
// left untouched, and only the fields config left empty are filled from the
// persisted firewall state. This is the core of SeedHostFirewallFromState /
// mergeHostFirewallFromState, so a regression here would let persisted state
// silently override an explicit --config value.
func TestApplyPersistedFirewallContent_ConfigWins(t *testing.T) {
	persisted := &models.HostConfig{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		BlockedCIDRs:    []string{"192.0.2.0/24"},
		SSHPort:         2222,
		PodCIDR:         "10.4.0.0/14",
		InClusterPorts:  []int{8080},
		Disabled:        true, // decision must NOT be applied by this helper
	}

	cfg := models.HostConfig{
		// Operator supplied a management allowlist and SSH port via --config;
		// everything else is left to fall back to state.
		ManagementCIDRs: []string{"203.0.113.0/24"},
		SSHPort:         22,
	}

	got := applyPersistedFirewallContent(cfg, persisted)

	assert.Equal(t, []string{"203.0.113.0/24"}, got.ManagementCIDRs, "config allowlist must win over state")
	assert.Equal(t, 22, got.SSHPort, "config SSH port must win over state")
	assert.Equal(t, []string{"192.0.2.0/24"}, got.BlockedCIDRs, "empty blocked CIDRs must fall back to state")
	assert.Equal(t, "10.4.0.0/14", got.PodCIDR, "empty pod CIDR must fall back to state")
	assert.Equal(t, []int{8080}, got.InClusterPorts, "empty in-cluster ports must fall back to state")
	assert.False(t, got.Disabled, "the enable/disable decision must not be touched by the content merge")
}

// TestApplyPersistedFirewallContent_NilPersistedIsNoOp verifies that a nil
// persisted firewall (fresh host, nothing ever recorded) leaves config untouched.
func TestApplyPersistedFirewallContent_NilPersistedIsNoOp(t *testing.T) {
	cfg := models.HostConfig{ManagementCIDRs: []string{"203.0.113.0/24"}}
	got := applyPersistedFirewallContent(cfg, nil)
	assert.Equal(t, cfg, got)
}

// TestResolveFirewallSeed is the truth table behind issue #1003. The row that
// matters most is "nothing recorded + a live table": before the fix that read as
// "disabled" and a no-flag `block node reconfigure` deleted a firewall the
// operator had created with `network firewall create`.
func TestResolveFirewallSeed(t *testing.T) {
	enabled := &models.HostConfig{Disabled: false}
	disabled := &models.HostConfig{Disabled: true}

	tests := []struct {
		name      string
		persisted *models.HostConfig
		active    bool
		want      bool
	}{
		{"nothing recorded, table live", nil, true, true},
		{"nothing recorded, no table", nil, false, false},
		{"recorded disabled, table live", disabled, true, true},
		{"recorded disabled, no table", disabled, false, false},
		{"recorded enabled, no table", enabled, false, true},
		{"recorded enabled, table live", enabled, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubFirewallManager(t, tt.active, nil)
			assert.Equal(t, tt.want, ResolveFirewallSeed(context.Background(), tt.persisted))
		})
	}
}

// TestMergeLiveHostFirewall_ConfigWinsStateLoses pins the new precedence tier:
// --config still beats the live firewall, but the live firewall beats whatever
// machine state recorded. Without that ordering a reconfigure's force re-render
// reverts an urgent `network firewall add --name mgmt --cidr …` back to the
// allowlist captured at install time.
func TestMergeLiveHostFirewall_ConfigWinsStateLoses(t *testing.T) {
	live := firewall.NewTable()
	live.Mgmt.CIDRs = []string{"10.9.0.0/16"}
	live.Mgmt.Ports = []string{"2222"}
	live.Blocked.CIDRs = []string{"198.51.100.0/24"}
	live.InCluster.CIDRs = []string{"10.4.0.0/14"}
	live.InCluster.Ports = []string{"4244", "6443"}
	stubFirewallManager(t, true, live)

	// The operator pinned only the blocked list via --config.
	cfg := models.HostConfig{BlockedCIDRs: []string{"203.0.113.0/24"}}
	cfg = mergeLiveHostFirewall(context.Background(), cfg)

	assert.Equal(t, []string{"203.0.113.0/24"}, cfg.BlockedCIDRs, "config must win over the live firewall")
	assert.Equal(t, []string{"10.9.0.0/16"}, cfg.ManagementCIDRs, "the live allowlist must fill an unset field")
	assert.Equal(t, 2222, cfg.SSHPort)
	assert.Equal(t, "10.4.0.0/14", cfg.PodCIDR)
	assert.Equal(t, []int{4244, 6443}, cfg.InClusterPorts)

	// State is only consulted for what is still empty after the live tier.
	cfg = applyPersistedFirewallContent(cfg, &models.HostConfig{
		ManagementCIDRs: []string{"192.168.50.0/24"},
		SSHPort:         22,
	})
	assert.Equal(t, []string{"10.9.0.0/16"}, cfg.ManagementCIDRs, "the live firewall must win over persisted state")
	assert.Equal(t, 2222, cfg.SSHPort)
}

// TestMergeLiveHostFirewall_NoLiveFirewallIsNoOp verifies the common case on a
// host that has never had a firewall: nothing to load, config untouched.
func TestMergeLiveHostFirewall_NoLiveFirewallIsNoOp(t *testing.T) {
	stubFirewallManager(t, false, nil)

	cfg := models.HostConfig{ManagementCIDRs: []string{"203.0.113.0/24"}}
	assert.Equal(t, cfg, mergeLiveHostFirewall(context.Background(), cfg))
}

// TestHostConfigFromTable_SkipsWhatHostConfigCannotHold covers the lossy edges of
// the projection. HostConfig's port fields are []int, so an inclusive range
// authored through `network firewall set --ports` cannot be carried; and its
// CIDR fields are IPv4-only, so seeding an IPv6 member would fail
// HostConfig.Validate and abort the whole reconfigure. Both are left out (with a
// warning) rather than carried across and rejected downstream.
func TestHostConfigFromTable_SkipsWhatHostConfigCannotHold(t *testing.T) {
	tbl := firewall.NewTable()
	tbl.Mgmt.CIDRs = []string{"192.168.50.0/24", "2001:db8::/32"}
	tbl.InCluster.Ports = []string{"4244", "2379-2380", "6443"}

	got := hostConfigFromTable(tbl)
	assert.Equal(t, []string{"192.168.50.0/24"}, got.ManagementCIDRs, "only IPv4 members are carried")
	assert.Equal(t, []int{4244, 6443}, got.InClusterPorts, "only plain-integer specs are carried")
	assert.NoError(t, got.Validate(), "the projection must always be a valid HostConfig")
}
