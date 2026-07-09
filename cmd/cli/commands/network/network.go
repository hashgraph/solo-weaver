// SPDX-License-Identifier: Apache-2.0

package network

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/network/firewall"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/network/policy"
	"github.com/spf13/cobra"
)

var networkCmd = &cobra.Command{
	Use:   "network",
	Short: "Manage node-level network state (firewall, policy, shaping, load balancing)",
	Long: "Manage node-level network state behind the solo-provisioner traffic shaper. " +
		"The firewall scope manages the node-agnostic `inet host` nftables table; the policy scope " +
		"manages per-category traffic rules in the `inet weaver` table. Both are generic primitives " +
		"driven by CIDRs and class names, not tied to any specific node type.",
	RunE: common.DefaultRunE, // runnable group; sub-commands inherit parent/root flags
}

func init() {
	networkCmd.AddCommand(firewall.GetCmd())
	networkCmd.AddCommand(policy.GetCmd())
}

// GetCmd returns the root of the `network` command group.
func GetCmd() *cobra.Command {
	return networkCmd
}
