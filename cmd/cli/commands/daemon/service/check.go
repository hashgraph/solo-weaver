// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check the health of the solo-provisioner-daemon service",
	Long: "Verify the daemon installation: unit file present, service enabled and running, " +
		"binary exists, sudoers entry in place, and Unix socket responding to /health.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewDaemonServiceCheckWorkflow()); err != nil {
			return err
		}

		logx.As().Info().Msg("solo-provisioner-daemon service is healthy")
		return nil
	},
}
