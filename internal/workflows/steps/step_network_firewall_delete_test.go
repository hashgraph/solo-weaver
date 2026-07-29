// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

// TestNetworkFirewallDelete_RemovesTable verifies the step deletes the live inet
// host table (and its on-disk artifact) via the firewall manager.
func TestNetworkFirewallDelete_RemovesTable(t *testing.T) {
	r := &fakeFwRunner{exists: true}
	nftPath := withStubbedFirewall(t, r)
	// Seed an on-disk artifact so we can assert the delete removes it.
	require.NoError(t, os.MkdirAll(filepath.Dir(nftPath), 0o755))
	require.NoError(t, os.WriteFile(nftPath, []byte("table inet host {}\n"), 0o644))

	step, err := NetworkFirewallDelete().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.True(t, r.deleted, "delete step should remove the live table")
	require.NoFileExists(t, nftPath, "delete step should remove the on-disk nft artifact")
}

// TestNetworkFirewallDelete_Idempotent verifies deleting when no table is present
// is a no-op success (firewall.Manager.Delete existence-checks first).
func TestNetworkFirewallDelete_Idempotent(t *testing.T) {
	r := &fakeFwRunner{exists: false}
	withStubbedFirewall(t, r)

	step, err := NetworkFirewallDelete().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.False(t, r.deleted, "no live table present, nothing to delete")
}
