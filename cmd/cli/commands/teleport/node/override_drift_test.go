// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// Only the destructive commands take --override-drift.
func TestOverrideDrift_OnlyOnDestructiveCommands(t *testing.T) {
	for _, cmd := range []*cobra.Command{uninstallCmd} {
		require.NotNil(t, cmd.Flags().Lookup(common.FlagOverrideDrift().Name), cmd.Name())
	}
	for _, cmd := range []*cobra.Command{installCmd} {
		require.Nil(t, cmd.Flags().Lookup(common.FlagOverrideDrift().Name), cmd.Name())
	}
}

// The uninstall's inputs carry --force and --override-drift to the handler.
func TestUninstallInputs_CarryTheGuardFlags(t *testing.T) {
	var force, override bool
	root := &cobra.Command{Use: "root"}
	common.FlagForce().SetVarP(root, &force, false)
	cmd := &cobra.Command{Use: "uninstall"}
	common.FlagOverrideDrift().SetVar(cmd, &override, false)
	root.AddCommand(cmd)
	require.NoError(t, root.PersistentFlags().Set(common.FlagForce().Name, "true"))
	require.NoError(t, cmd.Flags().Set(common.FlagOverrideDrift().Name, "true"))

	inputs, err := uninstallInputs(cmd, nil)

	require.NoError(t, err)
	require.True(t, inputs.Common.Force)
	require.True(t, inputs.Common.OverrideDrift)
	require.Equal(t, automa.StopOnError, inputs.Common.ExecutionOptions.ExecutionMode)
}
