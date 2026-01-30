// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/alloy/cluster"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	alloyCmd = &cobra.Command{
		Use:   "alloy",
		Short: "Manage Grafana Alloy observability stack",
		Long:  "Manage Grafana Alloy observability stack for metrics and logs collection. Includes Prometheus CRDs, Node Exporter, and Alloy.",
		RunE:  common.DefaultRunE, // provide a default action so the parent command is runnable and its PersistentPreRunE hooks are invoked; persistent flags are inherited by subcommands regardless
	}
)

func init() {
	alloyCmd.AddCommand(cluster.GetCmd())
}

func GetCmd() *cobra.Command {
	return alloyCmd
}
