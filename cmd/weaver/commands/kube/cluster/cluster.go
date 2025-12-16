// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagValuesFile    string
	flagMetricsServer bool

	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage lifecycle of a Kubernetes Cluster",
		Long:  "Manage lifecycle of a Kubernetes Cluster",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	clusterCmd.AddCommand(installCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
