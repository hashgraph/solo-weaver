// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestClusterInstallSizingFlagsRegisteredAndVisible pins the current contract:
// #807 dropped --profile/--node-type, but 97a13e9a reinstated them as functional
// flags that size the cluster-install preflight hardware floor. They must be
// registered AND visible in --help (not hidden) — cluster install reads --profile
// (with --node-type) to size the host for the intended workload.
func TestClusterInstallSizingFlagsRegisteredAndVisible(t *testing.T) {
	root := GetCmd()

	// --profile is persistent on the kube command; cluster install reads it to size
	// the preflight hardware floor for the intended workload.
	profile := root.PersistentFlags().Lookup("profile")
	require.NotNil(t, profile, "--profile must be registered")
	require.False(t, profile.Hidden, "--profile must be visible in --help")

	install := findSubcommand(t, findSubcommand(t, root, "cluster"), "install")

	// --node-type declares the workload; it drives dependency selection and, with
	// --profile, the preflight hardware sizing.
	nodeType := install.PersistentFlags().Lookup("node-type")
	require.NotNil(t, nodeType, "--node-type must be registered")
	require.False(t, nodeType.Hidden, "--node-type must be visible in --help")
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
