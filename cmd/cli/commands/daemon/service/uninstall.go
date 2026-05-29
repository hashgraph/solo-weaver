// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the solo-provisioner-daemon systemd service",
	Long:  "Disable and remove the solo-provisioner-daemon systemd service unit file from the local system.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return common.RunWorkflowBuilder(cmd.Context(), workflows.NewDaemonServiceUninstallWorkflow())
	},
}
