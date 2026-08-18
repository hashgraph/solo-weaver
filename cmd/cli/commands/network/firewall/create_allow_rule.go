// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var createAllowRuleCmd = &cobra.Command{
	Use:   "create-allow-rule",
	Short: "Declare a named allow rule, then populate it with `add`",
	Long: "Declare a named allow rule on the host firewall. The rule is created empty: `add --name <rule>` " +
		"supplies its addresses and ports afterwards, and both lists take comma-separated values, so one `add` " +
		"finishes the rule in a single atomic apply.\n\n" +
		"A declared rule renders no nft rule until it has at least one CIDR and either a port or --icmp-echo, so " +
		"running the declare and the populate as separate commands never opens access early.\n\n" +
		"With --force an existing rule is replaced outright: every field not supplied again returns to its " +
		"default, so --proto and --icmp-echo are reset along with the addresses and ports. Use `set` to change " +
		"one field of a populated rule.\n\n" +
		"Declaring is a separate verb from `create`, which states the whole table. It is also separate from `add`: " +
		"an unknown --name on add/remove/set keeps failing, so a typo edits nothing rather than quietly creating a " +
		"second rule alongside the intended one. The reserved blocks (" + strings.Join(fw.ReservedNames, ", ") +
		") cannot be declared this way — they always exist and are configured through `create` and `set`.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("name") {
			return errorx.IllegalArgument.New("--name is required: the name of the allow rule to declare")
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}

		// Only carry a field the operator actually set. Leaving Proto empty keeps
		// it absent from `show --output yaml` and lets the model apply its own tcp
		// default, so a declared rule round-trips through the config unchanged.
		r := fw.Rule{Name: flagName}
		if cmd.Flags().Changed("proto") {
			r.Proto = fw.Proto(flagProto)
		}
		if cmd.Flags().Changed("icmp-echo") {
			r.ICMPEcho = flagICMPEcho
		}

		changed, err := newManager().CreateRule(cmd.Context(), r, force)
		if err != nil {
			return err
		}

		if changed {
			logx.As().Info().Str("rule", r.Name).Msg(
				"allow rule declared; populate it with `network firewall add --name " + r.Name + " --cidr <cidr> --port <port>`")
		}
		return nil
	},
}

func init() {
	createAllowRuleCmd.Flags().StringVar(&flagName, "name", "",
		"Name of the allow rule to declare (may not be a reserved block: "+strings.Join(fw.ReservedNames, ", ")+")")
	createAllowRuleCmd.Flags().StringVar(&flagProto, "proto", "",
		"L4 protocol the rule's ports match: tcp or udp (default tcp)")
	createAllowRuleCmd.Flags().BoolVar(&flagICMPEcho, "icmp-echo", false,
		"Grant this rule's sources unmetered ICMP echo-request, above the rate meter")
}
