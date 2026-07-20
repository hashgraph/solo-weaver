// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestIsPrivilegeExemptInvocation(t *testing.T) {
	exempt := [][]string{
		{},
		{"--version"},
		{"-v"},
		{"--help"},
		{"-h"},
		{"version"},
		{"help"},
		{"help", "block"},
		{"--non-interactive", "--help"},
		{"--log-level", "debug", "-h"},
		{"block", "node", "reconcile-shaper", "--check", "--statusz-url", "http://127.0.0.1:8080"},
		{"block", "node", "reconcile-shaper", "--statusz-url=http://127.0.0.1:8080", "--check"},
		{"block", "node", "reconcile-shaper", "--check=true", "--statusz-url", "http://127.0.0.1:8080"},
		{"--log-level", "debug", "block", "node", "reconcile-shaper", "--check", "--statusz-url", "http://127.0.0.1:8080"},
	}
	for _, args := range exempt {
		require.True(t, isPrivilegeExemptInvocation(args), "expected exempt: %v", args)
	}

	notExempt := [][]string{
		{"block", "node", "install"},
		{"install"},
		{"uninstall"},
		{"kube", "cluster", "install"},
		{"block", "node", "upgrade", "--profile=mainnet"},
		{"teleport", "node", "install"},
		{"alloy", "cluster", "install"},
		{"block", "node", "reconcile-shaper", "--statusz-url", "http://127.0.0.1:8080"},
		{"block", "node", "reconcile-shaper", "--check=false", "--statusz-url", "http://127.0.0.1:8080"},
	}
	for _, args := range notExempt {
		require.False(t, isPrivilegeExemptInvocation(args), "expected not exempt: %v", args)
	}
}

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
