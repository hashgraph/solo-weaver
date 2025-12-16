// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Kubernetes Cluster",
	Long:    "Run safety checks, setup a K8s cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile.Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		logx.As().Debug().
			Strs("args", args).
			Str("profile", flagProfile).
			Str("valuesFile", flagValuesFile).
			Msg("Installing Kubernetes Cluster")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodeInstallWorkflow(
			flagProfile, flagValuesFile, workflows.ClusterSetupOptions{
				SetupMetricsServer: flagMetricsServer,
			}))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}

func init() {
	common.FlagMetricsServer.SetVarP(installCmd, &flagMetricsServer, false)
}
