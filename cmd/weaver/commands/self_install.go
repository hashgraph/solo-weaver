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
		logx.As().Info().Msg("Solo Provisioner is uninstalled successfully")
		return nil
	},
}
