// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Grafana Alloy observability stack",
	Long:  "Install the Grafana Alloy observability stack including Prometheus CRDs, Node Exporter, and Alloy for metrics and logs collection",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Apply Alloy configuration overrides
		alloyOverrides := config.AlloyConfig{
			MonitorBlockNode:   flagMonitorBlockNode,
			PrometheusURL:      flagPrometheusURL,
			PrometheusUsername: flagPrometheusUsername,
			LokiURL:            flagLokiURL,
			LokiUsername:       flagLokiUsername,
			ClusterName:        flagClusterName,
		}
		config.OverrideAlloyConfig(alloyOverrides)

		logx.As().Debug().
			Strs("args", args).
			Bool("monitorBlockNode", flagMonitorBlockNode).
			Str("prometheusURL", flagPrometheusURL).
			Str("lokiURL", flagLokiURL).
			Str("clusterName", flagClusterName).
			Msg("Installing Alloy observability stack")

		wb := workflows.NewAlloyInstallWorkflow()

		common.RunWorkflow(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully installed Alloy observability stack")
		return nil
	},
}

// Verify that the required step functions are accessible
var _ = steps.SetupAlloyStack
