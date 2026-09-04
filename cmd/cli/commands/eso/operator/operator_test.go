// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
)

// Registered once on the parent, so both subcommands inherit them and neither
// redeclares them.
func TestOperatorFlagsArePersistentAndInherited(t *testing.T) {
	names := []string{
		common.FlagESONamespace().Name,
		common.FlagStopOnError().Name,
		common.FlagRollbackOnError().Name,
		common.FlagContinueOnError().Name,
	}

	root := GetCmd()
	for _, name := range names {
		require.NotNil(t, root.PersistentFlags().Lookup(name),
			"--%s must be persistent on the eso operator command", name)
	}

	for _, sub := range []string{"install", "uninstall"} {
		t.Run(sub, func(t *testing.T) {
			cmd := findSubcommand(t, root, sub)
			require.NoError(t, cmd.InheritedFlags().Parse(nil))
			for _, name := range names {
				require.NotNil(t, cmd.InheritedFlags().Lookup(name),
					"%s must inherit --%s from the operator command", sub, name)
			}
			require.Nil(t, cmd.Flags().Lookup(common.FlagESONamespace().Name),
				"%s must not register its own --namespace", sub)
		})
	}
}

// ValidateFlagGroups is what cobra calls during execute(). One pair only: the
// flags are package-level vars on a shared command, so setting them would
// pollute further cases.
func TestErrorControlFlagsAreMutuallyExclusive(t *testing.T) {
	install := findSubcommand(t, GetCmd(), "install")

	// Flags() merges the parent's persistent set, annotations included.
	flags := install.Flags()
	require.NoError(t, flags.Set(common.FlagStopOnError().Name, "true"))
	require.NoError(t, flags.Set(common.FlagRollbackOnError().Name, "true"))

	require.Error(t, install.ValidateFlagGroups(),
		"--stop-on-error and --rollback-on-error must be mutually exclusive on the subcommand")
}

func findSubcommand(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	require.Failf(t, "subcommand not found", "%q under %q", name, parent.Name())
	return nil
}
