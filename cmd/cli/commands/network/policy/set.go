// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Atomically replace a policy's live set membership",
	Long: "Replace the full CIDR membership of a named policy's nft set in a single kernel transaction " +
		"(`flush set + add element` in one `nft -f -` document). Omitting `--cidrs`/`--cidrs-file` clears " +
		"the set. Like `add`/`remove`, only the live kernel set is mutated — no chain re-render and no " +
		"update to network-weaver.nft.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cidrs, err := resolveCIDRs(cmd)
		if err != nil {
			return err
		}
		if err := newManager().Set(cmd.Context(), flagName, cidrs); err != nil {
			return err
		}
		logx.As().Info().Str("policy", flagName).Msg("network policy set membership replaced")
		return nil
	},
}

func init() {
	setCmd.Flags().StringVar(&flagName, "name", "", "Policy name (required)")
	setCmd.Flags().StringSliceVar(&flagCIDRs, "cidrs", nil, "Replacement membership (comma-separated or repeated); omit to clear")
	setCmd.Flags().StringVar(&flagCIDRsFile, "cidrs-file", "", "Alternative to --cidrs: a file of CIDRs (one per line or comma-separated)")
	_ = setCmd.MarkFlagRequired("name")
}
