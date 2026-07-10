// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove one or more CIDRs from a policy's live set",
	Long: "Remove CIDRs from the live nft set for a named policy (`nft delete element inet weaver <name> { … }`). " +
		"Like `add`, only the live kernel set is mutated — no chain re-render and no update to network-weaver.nft. " +
		"Use `--cidr` one or more times, or pass a comma-separated list in a single `--cidr` flag.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(flagCIDR) == 0 {
			return errorx.IllegalArgument.New("--cidr is required")
		}
		if err := newManager().Remove(cmd.Context(), flagName, flagCIDR); err != nil {
			return err
		}
		logx.As().Info().Str("policy", flagName).Strs("cidrs", flagCIDR).Msg("network policy CIDRs removed")
		return nil
	},
}

func init() {
	removeCmd.Flags().StringVar(&flagName, "name", "", "Policy name (required)")
	removeCmd.Flags().StringSliceVar(&flagCIDR, "cidr", nil, "CIDR to remove (comma-separated or repeated)")
	_ = removeCmd.MarkFlagRequired("name")
}
