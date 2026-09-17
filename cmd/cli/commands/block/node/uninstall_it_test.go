// SPDX-License-Identifier: Apache-2.0

//go:build integration

package node

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPurgeStorageFlag_Registration confirms which block-node subcommands accept
// --purge-storage: the four that already own the storage they are about to
// clear. `install` and `check` have no deployed volumes to delete.
func TestPurgeStorageFlag_Registration(t *testing.T) {
	cases := []struct {
		name     string
		cmd      *cobra.Command
		expected bool
	}{
		{name: "uninstall_has_purge_storage", cmd: uninstallCmd, expected: true},
		{name: "reconfigure_has_purge_storage", cmd: reconfigureCmd, expected: true},
		{name: "upgrade_has_purge_storage", cmd: upgradeCmd, expected: true},
		{name: "reset_has_purge_storage", cmd: resetCmd, expected: true},
		{name: "install_does_not_have_purge_storage", cmd: installCmd, expected: false},
		{name: "check_does_not_have_purge_storage", cmd: checkCmd, expected: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.cmd.Flag(common.FlagPurgeStorage().Name)
			if tc.expected {
				require.NotNil(t, f, "expected --purge-storage to be registered on %q", tc.cmd.Use)
				assert.Equal(t, "false", f.DefValue, "--purge-storage should default to false")
			} else {
				assert.Nil(t, f, "did not expect --purge-storage on %q", tc.cmd.Use)
			}
		})
	}
}
