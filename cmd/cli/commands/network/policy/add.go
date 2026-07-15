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
	Long: "Add CIDRs to the live nft set for a named policy (`nft add element inet weaver <name> { … }`). " +
		"The set is mutated directly — no chain re-render and no update to network-weaver.nft, because " +
		"set membership is owned by the daemon and never persisted. " +
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
