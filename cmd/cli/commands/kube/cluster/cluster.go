// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var (
	// flagNodeType backs --node-type on `kube cluster install`. It declares the
	// workload(s) for hardware sizing (with --profile) and validation. Cluster
	// install no longer installs the solo-operator — that moved to
	// `kube operator install` — so --node-type only affects the preflight floor.
	flagNodeType        string
	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage lifecycle of a Kubernetes Cluster",
		Long:  "Manage lifecycle of a Kubernetes Cluster",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {

	clusterCmd.AddCommand(installCmd)
	clusterCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return clusterCmd
}
