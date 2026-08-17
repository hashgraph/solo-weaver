// SPDX-License-Identifier: Apache-2.0

// Package firewall wires the `solo-provisioner network firewall` verbs to the
// internal/network/firewall manager. The verbs manage the node-agnostic
// `inet weaver-host-firewall` nftables table: three reserved blocks (the
// management allowlist, the operator block list, the in-cluster host-service
// allowance) plus any number of named allow rules.
//
// The verbs split along a deliberate line. Structure — which rules exist, and
// what protocol each matches — is declared in a config file, because adding a
// rule is a reviewed change. Membership — the addresses and ports inside a rule
// — is mutable straight from the CLI, because unblocking an operator or opening
// a port is sometimes urgent.
package firewall

import (
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/joomcode/errorx"
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
	// Name-addressed flags, which reach any rule including the reserved blocks.
	// --cidr/--port (add, remove) and --cidrs/--ports (set) bind to the same
	// variables: the spelling differs to signal incremental vs replace, but the
	// value is a list either way.
	flagName      string
	flagCIDRs     []string
	flagPorts     []string
	flagCIDRsFile string
	flagFromFile  string
	flagOutput    string
	flagAll       bool

	// Per-block flags that predate --name, retained so every invocation that
	// worked before still works and the interactive install flow is unchanged.
	flagMgmtCIDRs      []string
	flagMgmtCIDR       string
	flagBlockedCIDRs   []string
	flagBlockedCIDR    string
	flagInClusterPorts []int
	flagInClusterPort  int
	flagSSHPort        int
	flagPodCIDR        []string
)

var firewallCmd = &cobra.Command{
	Use:   "firewall",
	Short: "Manage the node-level host firewall (`inet weaver-host-firewall` nftables table)",
	Long: "Manage the node-agnostic host firewall: the `inet weaver-host-firewall` nftables table that protects the " +
		"bare-metal host. It carries three reserved blocks — `mgmt` (management allowlist), `blocked` (operator " +
		"block list) and `in_cluster` (host-service ports reachable from the pod CIDR) — plus any number of named " +
		"allow rules declared in a config file. This table is separate from the `inet weaver-workload-policy` " +
		"workload plane and applies to every node type.",
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

// registerTargetFlags registers the name-addressed flags for an incremental verb
// (add, remove), where the singular spelling signals that the values are merged
// into the rule's existing lists rather than replacing them.
func registerTargetFlags(cmd *cobra.Command, verb string) {
	cmd.Flags().StringVar(&flagName, "name", "",
		"Rule to modify: a reserved block (mgmt, blocked, in_cluster) or a named allow rule")
	cmd.Flags().StringSliceVar(&flagCIDRs, "cidr", nil,
		"CIDR(s) to "+verb+" (comma-separated or repeated)")
	cmd.Flags().StringSliceVar(&flagPorts, "port", nil,
		"Port(s) to "+verb+"; a single port (6443) or an inclusive range (2379-2380) (comma-separated or repeated)")
}

// resolveTarget determines which rule an incremental verb operates on and the
// values to apply. --name addresses any rule; the per-block flags that predate
// it address their reserved block implicitly, which is what keeps older
// invocations working. The two forms are mutually exclusive — combining them
// would leave it ambiguous which rule the general --cidr belongs to.
func resolveTarget(cmd *cobra.Command) (name string, cidrs, ports []string, err error) {
	f := cmd.Flags()
	general := f.Changed("name") || f.Changed("cidr") || f.Changed("port")

	switch {
	case f.Changed("mgmt-cidr"):
		name, cidrs = fw.RuleMgmt, []string{flagMgmtCIDR}
	case f.Changed("blocked-cidr"):
		name, cidrs = fw.RuleBlocked, []string{flagBlockedCIDR}
	case f.Changed("in-cluster-port"):
		name, ports = fw.RuleInCluster, []string{strconv.Itoa(flagInClusterPort)}
	default:
		if !f.Changed("name") {
			return "", nil, nil, errorx.IllegalArgument.New(
				"--name is required: name a reserved block (%s) or an allow rule", strings.Join(fw.ReservedNames, ", "))
		}
		if !f.Changed("cidr") && !f.Changed("port") {
			return "", nil, nil, errorx.IllegalArgument.New("at least one of --cidr or --port is required")
		}
		return flagName, flagCIDRs, flagPorts, nil
	}

	if general {
		return "", nil, nil, errorx.IllegalArgument.New(
			"--mgmt-cidr, --blocked-cidr and --in-cluster-port already name the rule they edit; use --name with --cidr/--port instead of combining the two forms")
	}
	return name, cidrs, ports, nil
}
