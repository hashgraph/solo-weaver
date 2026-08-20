// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"fmt"
	"strings"

	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Output formats for `show`.
const (
	outputNft  = "nft"
	outputYAML = "yaml"
	// outputCommands is valid only with --name. It projects one allow rule as
	// the `network firewall` sequence that recreates it, which is safe to replay
	// against a populated table because `create-allow-rule` + `add` are additive.
	// There is no whole-table equivalent: `--output yaml` already round-trips
	// exactly through `create --from-file`, and a second whole-table artifact
	// would be one more thing to keep in lockstep with the rule model.
	outputCommands = "commands"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the live `inet weaver-host-firewall` table, or its config with --output yaml",
	Long: "Show the host firewall. By default this prints the live nftables ruleset, which is ground truth for what " +
		"the kernel is enforcing. --output yaml instead prints the declarative config the ruleset was rendered " +
		"from, in the exact schema `create --from-file` accepts — so `show --output yaml > rules.yaml` followed by " +
		"`create --from-file rules.yaml --force` is a no-op.\n\n" +
		"--name narrows the output to one rule, for inspection. That view is not a config file: re-applying a " +
		"single rule as if it were the whole config would delete every other allow rule.\n\n" +
		"--output commands requires --name and works on named allow rules only. It prints the sequence that " +
		"recreates that one rule — `create-allow-rule` followed by a single `add` carrying its addresses and " +
		"ports — which, unlike the yaml view of one rule, is safe to replay against a host that already has a " +
		"firewall: the sequence is additive and touches no other rule. Use it to carry one rule to other hosts; " +
		"use --output yaml to carry the whole table.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch flagOutput {
		case outputNft, outputYAML, outputCommands:
		default:
			return errorx.IllegalArgument.New("invalid --output %q: expected %q, %q or %q",
				flagOutput, outputNft, outputYAML, outputCommands)
		}

		// A command sequence addresses one rule by name. Without --name there is
		// nothing to narrow to, and the whole-table answer is already exact via
		// the config view, so point there rather than inventing a second one.
		if flagOutput == outputCommands && !cmd.Flags().Changed("name") {
			return errorx.IllegalArgument.New(
				"--output %s requires --name: it prints the sequence for one allow rule. For the whole table use "+
					"`show --output %s`, which re-applies exactly through `create --from-file`", outputCommands, outputYAML)
		}

		// --name addresses a rule, which only the config knows about — the kernel
		// dump has no notion of which rule a set belongs to — so it implies the
		// config view regardless of --output.
		if cmd.Flags().Changed("name") {
			return showRule(cmd)
		}
		if flagOutput == outputYAML {
			return showConfig(cmd)
		}

		out, err := newManager().Show(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

func showConfig(cmd *cobra.Command) error {
	cfg, err := newManager().Config(cmd.Context())
	if err != nil {
		return err
	}
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), string(data))
	return nil
}

func showRule(cmd *cobra.Command) error {
	t, err := newManager().Table(cmd.Context())
	if err != nil {
		return err
	}
	r, ok := t.Rule(flagName)
	if !ok {
		return errorx.IllegalArgument.New("no rule named %q; known rules are %s", flagName, strings.Join(t.Names(), ", "))
	}
	if flagOutput == outputCommands {
		return showRuleCommands(cmd, r)
	}
	data, err := yaml.Marshal(r)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to render rule %q as YAML", flagName)
	}
	fmt.Fprint(cmd.OutOrStdout(), string(data))
	return nil
}

// showRuleCommands prints the additive sequence that recreates one allow rule.
//
// Two commands at most, not one per value: --cidr and --port are both lists and
// Manager.Add applies them inside a single mutateRule, so one `add` finishes the
// rule in one atomic apply. Emitting a line per address would turn a populated
// rule into a dozen restarts of the nft unit for the same end state.
//
// No shell quoting is applied, and none is needed: Rule.Validate puts the name
// through sanity.ValidateIdentifier and every address and port spec through its
// own validator, so no token here can carry a shell metacharacter or a comma
// that would confuse the comma-separated list flags.
//
// No `sudo` prefix either. The sequence is meant to be saved and run as a script
// (`sudo sh rules.sh`), where a per-line sudo is noise; the binary name comes
// from the running command rather than a hardcoded one so a renamed or
// vendor-installed binary emits its own name.
func showRuleCommands(cmd *cobra.Command, r *fw.Rule) error {
	// The reserved blocks are configured, not declared: they always exist, and
	// `create-allow-rule` refuses them. A "subsequence" for mgmt would have to be
	// a whole-table `create`, which is the config view's job.
	if fw.IsReserved(r.Name) {
		return errorx.IllegalArgument.New(
			"%q is a reserved block, not an allow rule, so it has no declare-and-populate sequence; it is configured "+
				"by `create` and `set`. Use `show --output %s` to export the whole table, or `show --name %s` to inspect it",
			r.Name, outputYAML, r.Name)
	}

	bin := cmd.Root().Name()
	out := cmd.OutOrStdout()

	declare := bin + " network firewall create-allow-rule --name " + r.Name
	// Only fields the rule actually carries. An empty Proto is left out so a
	// rule on the tcp default emits the same declare line that produced it,
	// matching how `create-allow-rule` records only what was set.
	if r.Proto != "" {
		declare += " --proto " + string(r.Proto)
	}
	if r.ICMPEcho {
		declare += " --icmp-echo"
	}
	fmt.Fprintln(out, declare)

	// A declared-but-unpopulated rule stops here: there is nothing to add, and an
	// `add` with neither list would fail.
	if len(r.CIDRs) == 0 && len(r.Ports) == 0 {
		return nil
	}

	populate := bin + " network firewall add --name " + r.Name
	if len(r.CIDRs) > 0 {
		populate += " --cidr " + strings.Join(r.CIDRs, ",")
	}
	if len(r.Ports) > 0 {
		populate += " --port " + strings.Join(r.Ports, ",")
	}
	fmt.Fprintln(out, populate)
	return nil
}

func init() {
	showCmd.Flags().StringVar(&flagName, "name", "", "Show only this rule: a reserved block (mgmt, blocked, in_cluster) or a named allow rule")
	showCmd.Flags().StringVar(&flagOutput, "output", outputNft,
		"Output format: nft (the live ruleset), yaml (the declarative config), or commands (the sequence that "+
			"recreates one allow rule; requires --name)")
}
