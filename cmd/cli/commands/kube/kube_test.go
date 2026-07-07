// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestDeprecatedClusterInstallFlagsHiddenButAccepted pins the deprecation contract
// for #685: `--profile` and `--node-type` must remain registered (so existing scripts
// do not break with "unknown flag") but hidden from --help.
func TestDeprecatedClusterInstallFlagsHiddenButAccepted(t *testing.T) {
	root := GetCmd()

	// --profile is retained (hidden, persistent) on the kube command for backward
	// compatibility; cluster install ignores its value.
	profile := root.PersistentFlags().Lookup("profile")
	require.NotNil(t, profile, "--profile must remain registered for backward compatibility")
	require.True(t, profile.Hidden, "--profile must be hidden from --help")

	install := findSubcommand(t, findSubcommand(t, root, "cluster"), "install")

	// --node-type is retained (hidden) on cluster install; its value is ignored.
	nodeType := install.PersistentFlags().Lookup("node-type")
	require.NotNil(t, nodeType, "--node-type must remain registered for backward compatibility")
	require.True(t, nodeType.Hidden, "--node-type must be hidden from --help")
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
