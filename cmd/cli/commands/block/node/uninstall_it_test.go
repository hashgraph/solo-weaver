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
// --purge-storage. The flag is intentionally only available on `uninstall` and
// `reconfigure`; `reset` and `upgrade` keep K8s objects by design.
func TestPurgeStorageFlag_Registration(t *testing.T) {
	cases := []struct {
		name     string
		cmd      *cobra.Command
		expected bool
	}{
		{name: "uninstall_has_purge_storage", cmd: uninstallCmd, expected: true},
		{name: "reconfigure_has_purge_storage", cmd: reconfigureCmd, expected: true},
		{name: "upgrade_does_not_have_purge_storage", cmd: upgradeCmd, expected: false},
		{name: "reset_does_not_have_purge_storage", cmd: resetCmd, expected: false},
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
