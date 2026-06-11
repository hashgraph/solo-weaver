// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the solo-provisioner-daemon systemd service",
	Long:  "Stop the solo-provisioner-daemon systemd service via systemctl and verify it is no longer running. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewDaemonServiceStopWorkflow()); err != nil {
			return err
		}

		logx.As().Info().Msg("solo-provisioner-daemon service stopped")
		return nil
	},
}
