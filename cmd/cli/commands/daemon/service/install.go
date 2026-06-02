// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the solo-provisioner-daemon systemd service",
	Long: "Provision RBAC resources, generate the daemon kubeconfig, install and start the " +
		"solo-provisioner-daemon systemd service. Requires root privileges and a reachable K8s cluster.",
	RunE: func(cmd *cobra.Command, args []string) error {
		wf, err := workflows.NewDaemonServiceInstallWorkflow()
		if err != nil {
			return err
		}
		if err := common.RunWorkflowBuilder(cmd.Context(), wf); err != nil {
			return err
		}
		logx.As().Info().Msg("solo-provisioner-daemon service installed, enabled, and started")
		return nil
	},
}
