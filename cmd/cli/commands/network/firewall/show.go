// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"fmt"
	"strings"

	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Output formats for `show`.
const (
	outputNft  = "nft"
	outputYAML = "yaml"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the live `inet weaver-host-firewall` table, or its config with --output yaml",
	Long: "Show the host firewall. By default this prints the live nftables ruleset, which is ground truth for what " +
		"the kernel is enforcing. --output yaml instead prints the declarative config the ruleset was rendered " +
		"from, in the exact schema `create --from-file` accepts — so `show --output yaml > rules.yaml` followed by " +
		"`create --from-file rules.yaml --force` is a no-op.\n\n" +
		"--name narrows the output to one rule, for inspection. That view is not a config file: re-applying a " +
		"single rule as if it were the whole config would delete every other allow rule.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch flagOutput {
		case outputNft, outputYAML:
		default:
			return errorx.IllegalArgument.New("invalid --output %q: expected %q or %q", flagOutput, outputNft, outputYAML)
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
	data, err := yaml.Marshal(r)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to render rule %q as YAML", flagName)
	}
	fmt.Fprint(cmd.OutOrStdout(), string(data))
	return nil
}

func init() {
	showCmd.Flags().StringVar(&flagName, "name", "", "Show only this rule: a reserved block (mgmt, blocked, in_cluster) or a named allow rule")
	showCmd.Flags().StringVar(&flagOutput, "output", outputNft, "Output format: nft (the live ruleset) or yaml (the declarative config)")
}
