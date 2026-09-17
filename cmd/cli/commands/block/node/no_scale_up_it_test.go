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

// TestNoScaleUpFlag_Registration confirms which block-node subcommands accept
// --no-scale-up. It belongs on the three subcommands that stop the pod to do their
// work; `install` has nothing to scale back up and `uninstall` removes the
// release outright.
func TestNoScaleUpFlag_Registration(t *testing.T) {
	cases := []struct {
		name     string
		cmd      *cobra.Command
		expected bool
	}{
		{name: "reset_has_no_scale_up", cmd: resetCmd, expected: true},
		{name: "reconfigure_has_no_scale_up", cmd: reconfigureCmd, expected: true},
		{name: "upgrade_has_no_scale_up", cmd: upgradeCmd, expected: true},
		{name: "install_does_not_have_no_scale_up", cmd: installCmd, expected: false},
		{name: "uninstall_does_not_have_no_scale_up", cmd: uninstallCmd, expected: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.cmd.Flag(common.FlagNoScaleUp().Name)
			if tc.expected {
				require.NotNil(t, f, "expected --no-scale-up to be registered on %q", tc.cmd.Use)
				assert.Equal(t, "false", f.DefValue,
					"--no-scale-up must default to false so omitting it keeps the long-standing behaviour")
			} else {
				assert.Nil(t, f, "did not expect --no-scale-up on %q", tc.cmd.Use)
			}
		})
	}
}

// TestNoScaleUpFlag_DefaultVarIsFalse guards the package-level flagNoScaleUp var.
// prepareBlocknodeInputs is shared with install and check, which do not register
// the flag, so a true-valued var would report LeaveScaledDown on commands that
// never offered the choice.
func TestNoScaleUpFlag_DefaultVarIsFalse(t *testing.T) {
	assert.False(t, flagNoScaleUp,
		"flagNoScaleUp must stay false for the subcommands that do not register --no-scale-up")
}
