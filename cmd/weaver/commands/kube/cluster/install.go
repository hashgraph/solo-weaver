// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Kubernetes Cluster",
	Long:  "Run safety checks, setup a K8s cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Msg("Installing Kubernetes Cluster")

		opts := workflows.DefaultClusterSetupOptions()
		opts.SetupMetricsServer = flagMetricsServer
		common.RunWorkflow(cmd.Context(), workflows.NewSetupClusterWorkflow(opts))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}

func init() {
	common.FlagMetricsServer.SetVarP(installCmd, &flagMetricsServer, false)
}
