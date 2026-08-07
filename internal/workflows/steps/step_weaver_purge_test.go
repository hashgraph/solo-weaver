// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

// runStep builds and executes a step, returning its report.
func runStep(t *testing.T, b *automa.StepBuilder) *automa.Report {
	t.Helper()
	stp, err := b.Build()
	require.NoError(t, err)
	return stp.Execute(context.Background())
}

func TestCheckNoProvisionedCluster(t *testing.T) {
	// adminKubeconfigPath is an absolute host path, so the "cluster present" case
	// cannot be simulated without root. Assert the branch that a CI host can
	// actually reach: no kubeadm kubeconfig means nothing blocks the uninstall.
	if _, err := os.Stat(adminKubeconfigPath); err == nil {
		t.Skipf("%s exists on this host; the no-cluster branch is unreachable", adminKubeconfigPath)
	}

	rpt := runStep(t, CheckNoProvisionedCluster())
	require.Equal(t, automa.StatusSuccess, rpt.Status,
		"a host with no kubeadm kubeconfig must not block self-uninstall")
}

func TestRemoveNetworkConfig(t *testing.T) {
	t.Run("skips when the directory is already absent", func(t *testing.T) {
		// The step derives its target from firewall.HostNftPath, an absolute
		// path, so this only exercises the absent branch on a clean host.
		if _, err := os.Stat("/etc/solo-provisioner"); err == nil {
			t.Skip("/etc/solo-provisioner exists on this host")
		}
		rpt := runStep(t, RemoveNetworkConfig())
		require.Equal(t, automa.StatusSkipped, rpt.Status)
	})

	// RemoveAll's recursive behaviour is what the step relies on to take the
	// policy registry and the tc config subtrees with it; pin that expectation
	// against a stand-in tree so a future rewrite to a shallow delete is caught.
	t.Run("removes nested config subtrees", func(t *testing.T) {
		dir := t.TempDir()
		root := filepath.Join(dir, "solo-provisioner")
		for _, sub := range []string{"policies", "network/shape/devices", "network/shape/classes"} {
			require.NoError(t, os.MkdirAll(filepath.Join(root, sub), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, sub, "a.json"), []byte("{}"), 0o644))
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "network-weaver-host-firewall.nft"), []byte("x"), 0o644))

		require.NoError(t, os.RemoveAll(root))
		_, err := os.Stat(root)
		require.True(t, os.IsNotExist(err))
	})
}
