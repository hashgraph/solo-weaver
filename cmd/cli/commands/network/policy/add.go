// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add one or more CIDRs to a policy's live set",
	Long: "Add CIDRs to the live nft set for a named policy (`nft add element inet weaver-workload-policy <name> { … }`). " +
		"The set is mutated directly, without re-rendering the chain, and network-weaver-workload-policy.nft is then " +
		"rewritten with the set's new contents so the addition survives a reboot. " +
		"Note that for the sets the traffic-shaper daemon owns, it reconciles membership from the block node's statusz " +
		"on every poll, so a CIDR statusz does not report is removed again on the next tick; membership on any other " +
		"policy is left alone. " +
		"Use `--cidr` one or more times, or pass a comma-separated list in a single `--cidr` flag.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(flagCIDR) == 0 {
			return errorx.IllegalArgument.New("--cidr is required")
		}
		if err := newManager().Add(cmd.Context(), flagName, flagCIDR); err != nil {
			return err
		}
		logx.As().Info().Str("policy", flagName).Strs("cidrs", flagCIDR).Msg("network policy CIDRs added")
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&flagName, "name", "", "Policy name (required)")
	addCmd.Flags().StringSliceVar(&flagCIDR, "cidr", nil, "CIDR to add (comma-separated or repeated)")
	_ = addCmd.MarkFlagRequired("name")
}
