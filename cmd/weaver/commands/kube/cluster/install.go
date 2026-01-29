// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Kubernetes Cluster",
	Long:  "Run safety checks, setup a K8s cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile.Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		if flagProfile == "" {
			return errorx.IllegalArgument.New("profile flag is required")
		}

		// Set the profile in the global config so other components can access it
		config.SetProfile(flagProfile)

		// Apply configuration overrides from flags
		applyConfigOverrides()

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Any("opts", opts).
			Msg("Installing Kubernetes Cluster")

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(flagNodeType, flagProfile), opts)
		common.RunWorkflow(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
}

func init() {
	common.FlagNodeType.SetVarP(installCmd, &flagNodeType, true)
	common.FlagStopOnError.SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(installCmd, &flagContinueOnError, false)
}

// applyConfigOverrides applies flag values to override the configuration.
// This allows flags to take precedence over config file values.
// Note: Passwords are managed via Vault and External Secrets Operator, not via flags or env vars.
func applyConfigOverrides() {
	overrides := config.AlloyConfig{
		Enabled:            flagAlloyEnabled,
		MonitorBlockNode:   flagAlloyMonitorBlockNode,
		PrometheusURL:      flagAlloyPrometheusURL,
		PrometheusUsername: flagAlloyPrometheusUsername,
		LokiURL:            flagAlloyLokiURL,
		LokiUsername:       flagAlloyLokiUsername,
		ClusterName:        flagAlloyClusterName,
	}
	config.OverrideAlloyConfig(overrides)
}
