// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"errors"
	"strings"
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

// A RunE failure must not print cobra's usage block or its own error line —
// main.go renders errors once through doctor.CheckErr (#1035).
func TestRootCommandSilencesUsageAndErrors(t *testing.T) {
	require.True(t, rootCmd.SilenceUsage)
	require.True(t, rootCmd.SilenceErrors)
}

// A flag-parse error keeps cobra's error + usage output, with the error line
// first so a captured stderr leads with the cause.
func TestRootCommandFlagErrorFuncKeepsErrorAndUsage(t *testing.T) {
	probe := &cobra.Command{Use: "probe"}
	var buf bytes.Buffer
	probe.SetOut(&buf)
	probe.SetErr(&buf)

	parseErr := errors.New("unknown flag: --bogus")
	got := rootCmd.FlagErrorFunc()(probe, parseErr)

	require.ErrorIs(t, got, parseErr, "the error must flow on to doctor.CheckErr unchanged")
	errIdx := strings.Index(buf.String(), "unknown flag: --bogus")
	usageIdx := strings.Index(buf.String(), "Usage:")
	require.GreaterOrEqual(t, errIdx, 0)
	require.GreaterOrEqual(t, usageIdx, 0)
	require.Less(t, errIdx, usageIdx, "the cause must lead the output")
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
