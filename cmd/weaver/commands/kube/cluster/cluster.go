// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagNodeType        string
	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	// Alloy configuration flags
	flagAlloyEnabled            bool
	flagAlloyMonitorBlockNode   bool
	flagAlloyPrometheusURL      string
	flagAlloyPrometheusUsername string
	// Note: Password not exposed as flag - use ALLOY_PROMETHEUS_PASSWORD env var instead
	flagAlloyLokiURL      string
	flagAlloyLokiUsername string
	// Note: Password not exposed as flag - use ALLOY_LOKI_PASSWORD env var instead
	flagAlloyClusterName string

	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage lifecycle of a Kubernetes Cluster",
		Long:  "Manage lifecycle of a Kubernetes Cluster",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	// Alloy observability configuration flags
	clusterCmd.PersistentFlags().BoolVar(&flagAlloyEnabled, "alloy-enabled", false, "Enable Grafana Alloy for observability")
	clusterCmd.PersistentFlags().BoolVar(&flagAlloyMonitorBlockNode, "alloy-monitor-block-node", false, "Enable Block Node monitoring in Alloy")
	clusterCmd.PersistentFlags().StringVar(&flagAlloyPrometheusURL, "alloy-prometheus-url", "", "Prometheus remote write URL")
	clusterCmd.PersistentFlags().StringVar(&flagAlloyPrometheusUsername, "alloy-prometheus-username", "", "Prometheus username (use ALLOY_PROMETHEUS_PASSWORD env var for password)")
	clusterCmd.PersistentFlags().StringVar(&flagAlloyLokiURL, "alloy-loki-url", "", "Loki remote write URL")
	clusterCmd.PersistentFlags().StringVar(&flagAlloyLokiUsername, "alloy-loki-username", "", "Loki username (use ALLOY_LOKI_PASSWORD env var for password)")
	clusterCmd.PersistentFlags().StringVar(&flagAlloyClusterName, "alloy-cluster-name", "", "Cluster name for Alloy metrics/logs labels")

	clusterCmd.AddCommand(installCmd)
	clusterCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
