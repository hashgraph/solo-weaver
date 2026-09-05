// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the solo-operator on the cluster",
	Long: "Install the solo-operator Helm chart (and its bundled CRDs) into an existing cluster. " +
		"The operator's images are pulled with --image-pull-secret, a docker-registry Secret that " +
		"must already exist in the operator namespace. Run this after 'kube cluster install' and after " +
		"creating that secret (see 'task uat:operator:secrets').",
	RunE: func(cmd *cobra.Command, args []string) error {
		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Str("imagePullSecret", flagImagePullSecret).
			Any("opts", opts).
			Msg("Installing solo-operator")

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallOperatorWorkflow(flagImagePullSecret), opts)
		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully installed solo-operator")
		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&flagImagePullSecret, "image-pull-secret", models.ConsensusDefaultImagePullSecret,
		"Name of a docker-registry Secret in the operator namespace used to pull the operator's private images. Must already exist. Empty to disable (public images)")
	common.FlagStopOnError().SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(installCmd, &flagContinueOnError, false)
	installCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}
