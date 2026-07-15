// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestProfileFlagRegistered pins the wiring for #847: `alloy cluster` must expose a
// visible, persistent `--profile`/`-p` flag (mirroring the block command tree) so the
// deploy profile reaches config.Profile and the ops label profile can emit the
// `environment` label. Unlike kube's deprecated flag, this one must NOT be hidden.
func TestProfileFlagRegistered(t *testing.T) {
	root := GetCmd()

	profile := root.PersistentFlags().Lookup(common.FlagProfile().Name)
	require.NotNil(t, profile, "--profile must be registered on the alloy cluster command")
	require.False(t, profile.Hidden, "--profile must be visible in --help for alloy")
	require.Equal(t, common.FlagProfile().ShortName, profile.Shorthand, "--profile must keep its -p shorthand")

	// install inherits the persistent flag from its parent.
	install := findSubcommand(t, root, "install")
	root.SetArgs([]string{"install"})
	require.NoError(t, install.InheritedFlags().Parse(nil))
	require.NotNil(t, install.InheritedFlags().Lookup(common.FlagProfile().Name),
		"install must inherit --profile from the cluster command")
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
