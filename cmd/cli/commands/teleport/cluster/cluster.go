// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
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
	common.FlagTeleportVersion().SetVarP(clusterCmd, &flagVersion, false)
	common.FlagTeleportValuesFile().SetVarP(clusterCmd, &flagValuesFile, false)

	clusterCmd.AddCommand(installCmd)
	clusterCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
