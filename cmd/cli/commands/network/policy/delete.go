// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a policy (removes its rules, set, and registry entry; re-renders chain)",
	Long: "Remove a named policy from the `inet weaver` table: re-renders the chain without it, " +
		"applies the result to the live kernel, restores remaining policies' live membership, " +
		"removes the registry file, and atomically rewrites network-weaver.nft. " +
		"If this is the last policy, an empty chain (policy drop, no rules) is applied and the " +
		"boot oneshot is left enabled.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := newManager().Delete(cmd.Context(), flagName); err != nil {
			return err
		}
		logx.As().Info().Str("policy", flagName).Msg("network policy deleted")
		return nil
	},
}

func init() {
	deleteCmd.Flags().StringVar(&flagName, "name", "", "Policy name (required)")
	_ = deleteCmd.MarkFlagRequired("name")
}
