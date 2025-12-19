// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall a Kubernetes Cluster",
	Long:  "Uninstall a Kubernetes Cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Msg("Installing Kubernetes Cluster")

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultClusterSetupOptions()
		opts.SetupMetricsServer = flagMetricsServer
		opts.ExecutionMode = execMode

		common.RunWorkflow(cmd.Context(), workflows.NewTeardownClusterWorkflow(opts))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}

func init() {
	common.FlagMetricsServer.SetVarP(uninstallCmd, &flagMetricsServer, false)
	common.FlagStopOnError.SetVarP(uninstallCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(uninstallCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(uninstallCmd, &flagContinueOnError, false)
}
