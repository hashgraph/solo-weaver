// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestNoShortNameCollisionsInRealCommandTree(t *testing.T) {
	require.False(t, common.DetectShortNameCollisions(rootCmd),
		"short name collisions detected in command tree")
}

// TestVersionSubcommandSkipsGlobalChecks asserts that the registered
// `version` subcommand is annotated to bypass the global pre-run checks.
// Without this annotation the subcommand fails on freshly built binaries
// because the installation check runs first (see #615).
func TestVersionSubcommandSkipsGlobalChecks(t *testing.T) {
	var versionCmd *cobra.Command
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "version" {
			versionCmd = sub
			break
		}
	}
	require.NotNil(t, versionCmd, "version subcommand not registered on rootCmd")
	require.False(t, common.RequireGlobalChecks(versionCmd),
		"version subcommand must opt out of global pre-run checks")
}
