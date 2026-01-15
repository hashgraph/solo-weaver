// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"os"

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
// Passwords are read from environment variables for security (not exposed as flags).
func applyConfigOverrides() {
	// Read passwords from environment variables (more secure than command-line flags)
	prometheusPassword := os.Getenv("ALLOY_PROMETHEUS_PASSWORD")
	lokiPassword := os.Getenv("ALLOY_LOKI_PASSWORD")

	overrides := config.AlloyConfig{
		Enabled:            flagAlloyEnabled,
		MonitorBlockNode:   flagAlloyMonitorBlockNode,
		PrometheusURL:      flagAlloyPrometheusURL,
		PrometheusUsername: flagAlloyPrometheusUsername,
		PrometheusPassword: prometheusPassword,
		LokiURL:            flagAlloyLokiURL,
		LokiUsername:       flagAlloyLokiUsername,
		LokiPassword:       lokiPassword,
		ClusterName:        flagAlloyClusterName,
	}
	config.OverrideAlloyConfig(overrides)
}
