// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	// Alloy configuration flags
	flagMonitorBlockNode   bool
	flagPrometheusURL      string
	flagPrometheusUsername string
	flagLokiURL            string
	flagLokiUsername       string
	flagClusterName        string

	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage Alloy observability stack on Kubernetes cluster",
		Long:  "Manage Grafana Alloy observability stack for metrics and logs collection on a Kubernetes cluster",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	// Alloy observability configuration flags
	clusterCmd.PersistentFlags().BoolVar(&flagMonitorBlockNode, "monitor-block-node", false, "Enable Block Node monitoring in Alloy")
	clusterCmd.PersistentFlags().StringVar(&flagPrometheusURL, "prometheus-url", "", "Prometheus remote write URL")
	clusterCmd.PersistentFlags().StringVar(&flagPrometheusUsername, "prometheus-username", "", "Prometheus username (passwords managed via Vault)")
	clusterCmd.PersistentFlags().StringVar(&flagLokiURL, "loki-url", "", "Loki remote write URL")
	clusterCmd.PersistentFlags().StringVar(&flagLokiUsername, "loki-username", "", "Loki username (passwords managed via Vault)")
	clusterCmd.PersistentFlags().StringVar(&flagClusterName, "cluster-name", "", "Cluster name for Alloy metrics/logs labels")

	clusterCmd.AddCommand(installCmd)
	clusterCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
