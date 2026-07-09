// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show a policy's config and live set membership",
	Long: "Print the named policy's registry config (action, class, ports, created_at) followed by " +
		"its current live CIDR set membership from the kernel (`nft list set inet weaver <name>`). " +
		"No lock is taken — show is a read-only operation.",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newManager().Show(cmd.Context(), flagName)
		if err != nil {
			return err
		}
		cmd.Print(out)
		return nil
	},
}

func init() {
	showCmd.Flags().StringVar(&flagName, "name", "", "Policy name (required)")
	_ = showCmd.MarkFlagRequired("name")
}
