// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var (
	// Error-control flags (persistent → apply to both install and uninstall)
	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

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
	// Error-control flags (persistent on the cluster command, so both install and
	// uninstall inherit them — mirrors the block node pattern in block/node/node.go).
	common.FlagStopOnError().SetVarP(clusterCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(clusterCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(clusterCmd, &flagContinueOnError, false)
	clusterCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)

	// Core configuration flags
	common.FlagClusterName().SetVarP(clusterCmd, &flagClusterName, false)
	common.FlagMonitorBlockNode().SetVarP(clusterCmd, &flagMonitorBlockNode, false)

	// Deprecated: kept for backward compatibility but hidden
	common.FlagAlloyClusterSecretStore().SetVarPHidden(clusterCmd, &flagClusterSecretStore, false)

	// Multi-remote flags (repeatable)
	common.FlagPrometheusRemotes().SetVarP(clusterCmd, &flagPrometheusRemotes, false)
	common.FlagLokiRemotes().SetVarP(clusterCmd, &flagLokiRemotes, false)

	// Legacy single-remote flags (kept for backward compatibility)
	common.FlagPrometheusURL().SetVarP(clusterCmd, &flagPrometheusURL, false)
	common.FlagPrometheusUsername().SetVarP(clusterCmd, &flagPrometheusUsername, false)
	common.FlagLokiURL().SetVarP(clusterCmd, &flagLokiURL, false)
	common.FlagLokiUsername().SetVarP(clusterCmd, &flagLokiUsername, false)

	clusterCmd.AddCommand(installCmd)
	clusterCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
