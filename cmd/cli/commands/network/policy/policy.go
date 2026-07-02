// SPDX-License-Identifier: Apache-2.0

// Package policy wires the `solo-provisioner network policy` verbs to the
// internal/network/policy manager. It is a generic, category-agnostic primitive:
// the verbs manage per-category classification and ACL rules in the `inet weaver`
// nftables table (the workload traffic plane, design §7.2.4), keyed only on the
// operator-supplied --name, --stamp/--deny, --ports, and --cidrs. It knows
// nothing about block/consensus/mirror/relay nodes; today `block node install`
// is its only caller, but nothing in this package assumes that.
//
// This story (#758) implements `create` only; the element verbs
// (add/remove/set/show/delete) are Story 1.3 (#759).
package policy

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	pol "github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/spf13/cobra"
)

// Shared flag binding targets for the create verb. Only one verb runs per
// invocation, so reusing these across future verbs is safe.
var (
	flagName       string
	flagStamp      string
	flagDeny       bool
	flagReplyStamp string
	flagFromEntity string
	flagPorts      []string
	flagCIDRs      []string
	flagCIDRsFile  string
	flagPodCIDR    string
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage per-category traffic policies (`inet weaver` nftables table)",
	Long: "Manage the workload traffic plane: named policies in the `inet weaver` nftables table that " +
		"map source CIDRs (or any source) to an HTB priority class on a set of ports, or quarantine a set " +
		"of CIDRs. This table is separate from the `inet host` node firewall.",
	RunE: common.DefaultRunE,
}

func init() {
	policyCmd.AddCommand(createCmd)
}

// GetCmd returns the root of the `network policy` command group.
func GetCmd() *cobra.Command {
	return policyCmd
}

// newManager constructs the production manager (live nft kernel apply + systemd
// service enable). Indirected through a var so command tests can stub it.
var newManager = func() *pol.Manager { return pol.NewManager() }
