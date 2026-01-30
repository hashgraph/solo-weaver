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
	Short: "Uninstall Grafana Alloy observability stack",
	Long:  "Uninstall the Grafana Alloy observability stack including Node Exporter and Prometheus CRDs",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Msg("Uninstalling Alloy observability stack")

		wb := workflows.NewAlloyUninstallWorkflow()

		common.RunWorkflow(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully uninstalled Alloy observability stack")
		return nil
	},
}
