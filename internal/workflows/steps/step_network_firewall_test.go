// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// fakeFwRunner is an in-memory firewall.Runner for the step unit test so the
// step can be exercised on any platform without touching the kernel or systemd.
type fakeFwRunner struct {
	exists  bool
	deleted bool
}

func (f *fakeFwRunner) List(context.Context) (string, error) { return "", nil }
func (f *fakeFwRunner) Delete(context.Context) error         { f.deleted = true; f.exists = false; return nil }
func (f *fakeFwRunner) Exists(context.Context) (bool, error) { return f.exists, nil }

// withStubbedFirewall points newFirewallManager at a manager wired to the given
// fake runner, temp paths, and a no-op service apply, restoring it on cleanup.
// It returns the on-disk nft path so tests can assert the artifact was written.
func withStubbedFirewall(t *testing.T, r *fakeFwRunner) string {
	t.Helper()
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-host.nft")
	orig := newFirewallManager
	newFirewallManager = func() *firewall.Manager {
		return firewall.NewManagerWithConfig(firewall.Config{
			Runner:          r,
			NftPath:         nftPath,
			LockPath:        filepath.Join(dir, "lock"),
			ApplyViaService: func(context.Context) error { return nil },
		})
	}
	t.Cleanup(func() { newFirewallManager = orig })
	return nftPath
}

// setHostConfig replaces the global host config for the duration of the test.
func setHostConfig(t *testing.T, h models.HostConfig) {
	t.Helper()
	saved := config.Get()
	cfg := saved
	cfg.Host = h
	require.NoError(t, config.Set(&cfg))
	t.Cleanup(func() { _ = config.Set(&saved) })
}

func TestNetworkFirewallCreate_SkipsWithoutMgmtCIDRs(t *testing.T) {
	r := &fakeFwRunner{}
	nftPath := withStubbedFirewall(t, r)
	setHostConfig(t, models.HostConfig{}) // no management CIDRs

	step, err := NetworkFirewallCreate().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSkipped, report.Status)
	// Nothing must have been rendered — a default-drop table with an empty SSH
	// allowlist would lock the host out.
	require.NoFileExists(t, nftPath)
}

func TestNetworkFirewallCreate_SkipsWhenDisabled(t *testing.T) {
	// An explicit --firewall-enabled=false opt-out must skip even when a
	// management allowlist is configured (Disabled is checked first).
	r := &fakeFwRunner{}
	nftPath := withStubbedFirewall(t, r)
	setHostConfig(t, models.HostConfig{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		Disabled:        true,
	})

	step, err := NetworkFirewallCreate().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSkipped, report.Status)
	require.NoFileExists(t, nftPath)
}

func TestNetworkFirewallCreate_CreatesWhenMgmtCIDRsSet(t *testing.T) {
	r := &fakeFwRunner{exists: false}
	nftPath := withStubbedFirewall(t, r)
	setHostConfig(t, models.HostConfig{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		SSHPort:         22,
		PodCIDR:         models.DefaultClusterPodCIDR,
		InClusterPorts:  []int{6443, 10250},
	})

	step, err := NetworkFirewallCreate().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.FileExists(t, nftPath)

	// Create applies the ruleset via the (stubbed) systemd service, so the table
	// is now present in the kernel — model that on the fake so Delete sees it.
	r.exists = true

	// Rollback must delete the table this step created and remove the artifact.
	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	require.Equal(t, automa.StatusSuccess, rollback.Status)
	require.True(t, r.deleted, "rollback should delete the table created by this step")
	require.NoFileExists(t, nftPath, "rollback should remove the on-disk nft artifact")
}

func TestNetworkFirewallCreate_ExplicitEmptyPortsAndPodCIDROverrideDefaults(t *testing.T) {
	// An explicit empty PodCIDR/InClusterPorts (e.g. `--pod-cidr=` /
	// `--in-cluster-ports=` clearing a config-file value) must take effect
	// rather than silently falling back to NewTable()'s defaults.
	r := &fakeFwRunner{}
	nftPath := withStubbedFirewall(t, r)
	setHostConfig(t, models.HostConfig{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		SSHPort:         22,
		PodCIDR:         "",
		InClusterPorts:  nil,
	})

	step, err := NetworkFirewallCreate().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	rendered, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.NotContains(t, string(rendered), "6443", "default in-cluster ports must not leak in when explicitly cleared")
	require.NotContains(t, string(rendered), "tcp dport @in_cluster_ports", "the in-cluster-ports rule must be omitted when PodCIDR is explicitly empty")
}

func TestNetworkFirewallCreate_RollbackSkipsWhenPreexisting(t *testing.T) {
	// Table already exists → create-if-missing makes no change → rollback must
	// not delete a table this step did not create.
	r := &fakeFwRunner{exists: true}
	withStubbedFirewall(t, r)
	setHostConfig(t, models.HostConfig{ManagementCIDRs: []string{"10.0.0.0/8"}, SSHPort: 22})

	step, err := NetworkFirewallCreate().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	rollback := step.Rollback(context.Background())
	require.Equal(t, automa.StatusSkipped, rollback.Status)
	require.False(t, r.deleted, "rollback must not delete a pre-existing table")
}
