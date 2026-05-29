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
	Long:  "Install, enable, and start the solo-provisioner-daemon systemd service unit file on the local system.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewDaemonServiceInstallWorkflow()); err != nil {
			return err
		}

		logx.As().Info().Msg("solo-provisioner-daemon service installed, enabled, and started")
		return nil
	},
}
