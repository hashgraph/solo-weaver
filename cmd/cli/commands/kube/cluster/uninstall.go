// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall a Kubernetes Cluster",
	Long:  "Teardown the K8s cluster, stop services, remove bind mounts, and cleanup configuration files while preserving downloads cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Tearing down the cluster is destructive and irreversible — confirm first,
		// unless --force (or a non-interactive/no-TTY session, via ShouldPrompt).
		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}
		if prompt.ShouldPrompt(force) {
			ok, err := prompt.RunConfirm(
				"Uninstall the Kubernetes cluster?",
				"This tears down the cluster (kubeadm reset), stops services, removes bind mounts, and cleans up "+
					"configuration — destroying all workloads and cluster state on this host. The downloads cache is preserved.",
				false)
			if err != nil {
				return err
			}
			if !ok {
				return errorx.IllegalState.New("aborted: Kubernetes cluster not uninstalled")
			}
		}

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Any("opts", opts).
			Msg("Uninstalling Kubernetes Cluster")

		wb := workflows.WithWorkflowExecutionMode(workflows.UninstallClusterWorkflow(), opts)
		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully uninstalled Kubernetes Cluster")
		return nil
	},
}

func init() {
	common.FlagStopOnError().SetVarP(uninstallCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(uninstallCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(uninstallCmd, &flagContinueOnError, false)
	uninstallCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}
