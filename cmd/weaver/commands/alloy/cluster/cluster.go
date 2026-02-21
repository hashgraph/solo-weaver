// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	// Alloy configuration flags
	flagMonitorBlockNode   bool
	flagClusterName        string
	flagClusterSecretStore string

	// Legacy single-remote flags (deprecated, use --add-prometheus-remote and --add-loki-remote instead)
	flagPrometheusURL      string
	flagPrometheusUsername string
	flagLokiURL            string
	flagLokiUsername       string

	// Multi-remote flags (repeatable)
	flagPrometheusRemotes []string
	flagLokiRemotes       []string

	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage Alloy observability stack on Kubernetes cluster",
		Long:  "Manage Grafana Alloy observability stack for metrics and logs collection on a Kubernetes cluster",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	// Core configuration flags
	clusterCmd.PersistentFlags().StringVar(&flagClusterName, "cluster-name", "", "Cluster name for Alloy metrics/logs labels")
	clusterCmd.PersistentFlags().BoolVar(&flagMonitorBlockNode, "monitor-block-node", false, "Enable Block Node monitoring in Alloy")

	// Deprecated: kept for backward compatibility but hidden
	clusterCmd.PersistentFlags().StringVar(&flagClusterSecretStore, "cluster-secret-store", "vault-secret-store", "Name of the ClusterSecretStore resource")
	_ = clusterCmd.PersistentFlags().MarkHidden("cluster-secret-store")

	// Multi-remote flags (repeatable)
	clusterCmd.PersistentFlags().StringArrayVar(&flagPrometheusRemotes, "add-prometheus-remote", nil,
		"Add a Prometheus remote (format: name=<name>,url=<url>,username=<username>). Can be specified multiple times. "+
			"Password is expected in K8s Secret 'grafana-alloy-secrets' under key 'PROMETHEUS_PASSWORD_<NAME>'")
	clusterCmd.PersistentFlags().StringArrayVar(&flagLokiRemotes, "add-loki-remote", nil,
		"Add a Loki remote (format: name=<name>,url=<url>,username=<username>). Can be specified multiple times. "+
			"Password is expected in K8s Secret 'grafana-alloy-secrets' under key 'LOKI_PASSWORD_<NAME>'")

	// Legacy single-remote flags (kept for backward compatibility)
	clusterCmd.PersistentFlags().StringVar(&flagPrometheusURL, "prometheus-url", "", "Prometheus remote write URL (deprecated: use --add-prometheus-remote)")
	clusterCmd.PersistentFlags().StringVar(&flagPrometheusUsername, "prometheus-username", "", "Prometheus username (deprecated: use --add-prometheus-remote)")
	clusterCmd.PersistentFlags().StringVar(&flagLokiURL, "loki-url", "", "Loki remote write URL (deprecated: use --add-loki-remote)")
	clusterCmd.PersistentFlags().StringVar(&flagLokiUsername, "loki-username", "", "Loki username (deprecated: use --add-loki-remote)")

	clusterCmd.AddCommand(installCmd)
	clusterCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
