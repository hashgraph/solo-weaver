// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show policy config and live set membership (all policies, or one with --name)",
	Long: "Without --name, list every configured policy. With --name, print just that policy's " +
		"registry config (action, class, ports, created_at) followed by its current live CIDR set " +
		"membership from the kernel (`nft list set inet weaver-workload-policy <name>`). No lock is taken — show is a " +
		"read-only operation. This mirrors `network shape show`, where a bare `show` lists everything " +
		"and flags narrow the scope.",
	RunE: func(cmd *cobra.Command, args []string) error {
		m := newManager()
		var out string
		var err error
		if flagName == "" {
			out, err = m.ShowAll(cmd.Context())
		} else {
			out, err = m.Show(cmd.Context(), flagName)
		}
		if err != nil {
			return err
		}
		cmd.Print(out)
		return nil
	},
}

func init() {
	showCmd.Flags().StringVar(&flagName, "name", "", "Policy name (omit to list all policies)")
}
