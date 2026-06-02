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
	Long: "Stop and disable the solo-provisioner-daemon systemd service, remove the daemon kubeconfig, " +
		"and delete the K8s RBAC resources (ServiceAccount, ClusterRole, ClusterRoleBinding, token Secret). " +
		"Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		wf, err := workflows.NewDaemonServiceUninstallWorkflow()
		if err != nil {
			return err
		}
		return common.RunWorkflowBuilder(cmd.Context(), wf)
	},
}
