// SPDX-License-Identifier: Apache-2.0

// Package firewall wires the `solo-provisioner network firewall` verbs to the
// internal/network/firewall manager. The verbs manage the node-agnostic
// `inet host` nftables table (SSH/mgmt allowlist, ICMP policy, in-cluster
// host-service ports).
package firewall

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/spf13/cobra"
)

// Shared flag binding targets. Only one verb runs per invocation, so reusing
// the same variables for value-passing across verbs is safe. Caveat: pflag
// writes the flag's default into the bound variable at registration time, and
// every verb's init() runs — so when two verbs bind the same variable with
// different defaults (create defaults --in-cluster-ports to the stack set, set
// defaults it to nil) the last registration wins the variable's initial value.
// Verbs must therefore take their defaults from the model (NewTable) and gate
// overrides on cmd.Flags().Changed(), never trust the shared variable's default.
var (
	flagMgmtCIDRs      []string
	flagMgmtCIDR       string
	flagBlockedCIDRs   []string
	flagBlockedCIDR    string
	flagInClusterPorts []int
	flagInClusterPort  int
	flagSSHPort        int
	flagPodCIDR        string
)

var firewallCmd = &cobra.Command{
	Use:   "firewall",
	Short: "Manage the node-level host firewall (`inet host` nftables table)",
	Long: "Manage the node-agnostic host firewall: the `inet host` nftables table that protects the " +
		"bare-metal host (SSH/management allowlist, ICMP policy, in-cluster host-service ports). " +
		"This table is separate from the `inet weaver` workload plane and applies to every node type.",
	RunE: common.DefaultRunE,
}

func init() {
	firewallCmd.AddCommand(createCmd, addCmd, removeCmd, setCmd, showCmd, deleteCmd)
}

// GetCmd returns the root of the `network firewall` command group.
func GetCmd() *cobra.Command {
	return firewallCmd
}

// newManager constructs the production manager (live nft kernel apply + systemd
// service enable). Indirected through a var so command tests can stub it.
var newManager = func() *fw.Manager { return fw.NewManager() }
