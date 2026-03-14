// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var selfInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Perform self-installation of Solo Provisioner",
	Long:  "Perform self-installation of Solo Provisioner on the local system",
	RunE: func(cmd *cobra.Command, args []string) error {
		common.RunWorkflow(cmd.Context(), workflows.NewSelfInstallWorkflow())

		// RunGlobalChecks (and therefore RunStartupMigrations) is skipped for
		// the install command, so we invoke it explicitly here. This ensures
		// startup-scoped migrations such as LegacyBinaryMigration run even when
		// the user is installing or re-installing.
		if err := common.RunStartupMigrations(cmd.Context()); err != nil {
			return err
		}

		logx.As().Info().Msg("Solo Provisioner is installed successfully; run 'solo-provisioner -h' to see available commands")
		return nil
	},
}

var selfUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Solo Provisioner from the local system",
	Long:  "Uninstall Solo Provisioner from the local system",
	RunE: func(cmd *cobra.Command, args []string) error {
		common.RunWorkflow(cmd.Context(), workflows.NewSelfUninstallWorkflow())
		return nil
	},
}
