// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove CIDRs and/or ports from a rule (--name)",
	Long: "Remove addresses and/or ports from one rule of the host firewall. --name selects the rule: a reserved " +
		"block (mgmt, blocked, in_cluster) or a named allow rule. Removing an entry that is not there is a no-op.\n\n" +
		"Removing the last address (or last port) from the management allowlist is refused while the rule can still " +
		"match traffic, because the input chain is policy drop and this host would drop every new SSH connection. " +
		"Pass --force to do it anyway; a rule already made unreachable stays freely editable.\n\n" +
		"Ports are removed by exact spec, so removing 2379 from a rule holding the range 2379-2380 does nothing — " +
		"replace the range with `set --ports` instead of splitting it implicitly.",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, cidrs, ports, err := resolveTarget(cmd)
		if err != nil {
			return err
		}
		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}
		return newManager().Remove(cmd.Context(), name, cidrs, ports, force)
	},
}

func init() {
	registerTargetFlags(removeCmd, "remove")
	removeCmd.Flags().StringVar(&flagMgmtCIDR, "mgmt-cidr", "", "A single management CIDR to remove (shorthand for --name mgmt --cidr)")
	removeCmd.Flags().StringVar(&flagBlockedCIDR, "blocked-cidr", "", "A single operator block-list CIDR to remove (shorthand for --name blocked --cidr)")
	removeCmd.Flags().IntVar(&flagInClusterPort, "in-cluster-port", 0, "A single in-cluster host-service port to remove (shorthand for --name in_cluster --port)")
	removeCmd.MarkFlagsMutuallyExclusive("mgmt-cidr", "blocked-cidr", "in-cluster-port")
}
