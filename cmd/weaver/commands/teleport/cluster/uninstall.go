// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Teleport Kubernetes cluster agent",
	Long:  "Uninstall the Teleport Kubernetes cluster agent and remove its Helm release",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Msg("Uninstalling Teleport cluster agent")

		wb := workflows.NewTeleportClusterAgentUninstallWorkflow()

		common.RunWorkflowBuilder(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully uninstalled Teleport cluster agent")
		return nil
	},
}
