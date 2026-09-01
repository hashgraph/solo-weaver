// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add CIDRs and/or ports to a rule (--name), merging with what is already there",
	Long: "Add addresses and/or ports to one rule of the host firewall. --name selects the rule: a reserved block " +
		"(mgmt, blocked, in_cluster) or a named allow rule. Adding is idempotent — an entry already present is " +
		"left alone.\n\n" +
		"The --mgmt-cidr, --blocked-cidr and --in-cluster-port flags are retained shorthands that name their " +
		"reserved block implicitly.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		name, cidrs, ports, err := resolveTarget(cmd)
		if err != nil {
			return err
		}
		return newManager().Add(cmd.Context(), name, cidrs, ports)
	},
}

func init() {
	registerTargetFlags(addCmd, "add")
	addCmd.Flags().StringVar(&flagMgmtCIDR, "mgmt-cidr", "", "A single management CIDR or FQDN to add (shorthand for --name mgmt --cidr)")
	addCmd.Flags().StringVar(&flagBlockedCIDR, "blocked-cidr", "", "A single operator block-list CIDR to add (shorthand for --name blocked --cidr)")
	addCmd.Flags().IntVar(&flagInClusterPort, "in-cluster-port", 0, "A single in-cluster host-service port to add (shorthand for --name in_cluster --port)")
	addCmd.MarkFlagsMutuallyExclusive("mgmt-cidr", "blocked-cidr", "in-cluster-port")
}
