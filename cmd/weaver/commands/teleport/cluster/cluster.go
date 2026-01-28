// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	// Teleport cluster agent configuration flags
	flagVersion    string
	flagValuesFile string

	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage Teleport Kubernetes cluster agent",
		Long:  "Manage Teleport Kubernetes cluster agent for secure kubectl access and audit logging",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	// Cluster agent configuration flags
	clusterCmd.PersistentFlags().StringVar(&flagVersion, "version", "", "Teleport Helm chart version (default: 18.6.4)")
	clusterCmd.PersistentFlags().StringVar(&flagValuesFile, "values", "", "Path to Teleport Helm values file (required)")

	clusterCmd.AddCommand(installCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
