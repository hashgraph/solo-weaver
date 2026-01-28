// SPDX-License-Identifier: Apache-2.0

package teleport

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/teleport/cluster"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/teleport/node"
	"github.com/spf13/cobra"
)

var (
	teleportCmd = &cobra.Command{
		Use:   "teleport",
		Short: "Manage Teleport agents for secure access",
		Long:  "Manage Teleport agents for secure access. Includes node-level SSH agent and Kubernetes cluster agent.",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable so that sub-commands would inherit parent flags
	}
)

func init() {
	teleportCmd.AddCommand(node.GetCmd())
	teleportCmd.AddCommand(cluster.GetCmd())
}

func GetCmd() *cobra.Command {
	return teleportCmd
}
