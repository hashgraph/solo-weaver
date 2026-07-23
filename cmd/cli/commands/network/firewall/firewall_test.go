// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// captureRunner satisfies the Runner interface without touching the kernel.
// Apply is intentionally absent — live rule application goes through
// applyViaService (file write + service restart), not the Runner.
type captureRunner struct {
	exists bool
}

func (c *captureRunner) List(_ context.Context) (string, error) { return "", nil }
func (c *captureRunner) Delete(_ context.Context) error         { c.exists = false; return nil }
func (c *captureRunner) Exists(_ context.Context) (bool, error) { return c.exists, nil }

func TestFirewallCmd_Structure(t *testing.T) {
	cmd := GetCmd()
	require.Equal(t, "firewall", cmd.Use)

	want := map[string]bool{"create": false, "add": false, "remove": false, "set": false, "show": false, "delete": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Use]; ok {
			want[sub.Use] = true
		}
	}
	for verb, found := range want {
		require.True(t, found, "verb %q not registered under firewall", verb)
	}
}

func TestCreateCmd_Flags(t *testing.T) {
	for _, name := range []string{"mgmt-cidrs", "blocked-cidrs", "in-cluster-ports", "ssh-port", "pod-cidr"} {
		require.NotNil(t, createCmd.Flags().Lookup(name), "create is missing --%s", name)
	}
	// Defaults must match the firewall package defaults.
	require.Equal(t, "22", createCmd.Flags().Lookup("ssh-port").DefValue)
	// ICMP is a static ruleset, not flag-driven: there must be no icmp toggles.
	require.Nil(t, createCmd.Flags().Lookup("icmp-mgmt"), "icmp-mgmt flag should be removed")
	require.Nil(t, createCmd.Flags().Lookup("icmp-public"), "icmp-public flag should be removed")
}

func TestCreateCmd_DefaultsInClusterPortsWhenNotPassed(t *testing.T) {
	// Regression: `create` (even with --force) without --in-cluster-ports must
	// render the stack port set. The flag-binding var is shared with `set`
	// (nil default), which clobbers create's default in the shared variable —
	// so create must source the default from NewTable(), gated on Changed().
	// This executes the real command so the shared-var registration is exercised.
	r := &captureRunner{}
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-host.nft")

	origMgr, origDetect := newManager, detectPodCIDR
	newManager = func() *fw.Manager {
		return fw.NewManagerWithConfig(fw.Config{
			Runner:   r,
			NftPath:  nftPath,
			LockPath: filepath.Join(dir, ".applying"),
			ApplyViaService: func(context.Context) error {
				r.exists = true
				return nil
			},
		})
	}
	// No cluster reachable → pod rule omitted; the in_cluster_ports *set* still
	// renders its elements, which is what we assert on.
	detectPodCIDR = func(context.Context) (string, error) { return "", errors.New("no cluster") }
	defer func() { newManager, detectPodCIDR = origMgr, origDetect }()

	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().Bool("force", false, "force")
	root.AddCommand(GetCmd())
	root.SetArgs([]string{"firewall", "create", "--mgmt-cidrs", "10.0.0.0/8"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())

	data, err := os.ReadFile(nftPath)
	require.NoError(t, err, "create should have written the nft file")
	require.Contains(t, string(data), "elements = { 4244, 6443, 7472, 10250 }",
		"create without --in-cluster-ports must default to the stack port set")
}

func TestAddRemoveCmd_Flags(t *testing.T) {
	for _, c := range []string{"add", "remove"} {
		var cmd = addCmd
		if c == "remove" {
			cmd = removeCmd
		}
		require.NotNil(t, cmd.Flags().Lookup("mgmt-cidr"), "%s missing --mgmt-cidr", c)
		require.NotNil(t, cmd.Flags().Lookup("blocked-cidr"), "%s missing --blocked-cidr", c)
		require.NotNil(t, cmd.Flags().Lookup("in-cluster-port"), "%s missing --in-cluster-port", c)
	}
}
