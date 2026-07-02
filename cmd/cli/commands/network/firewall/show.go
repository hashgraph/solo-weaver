// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"fmt"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the live `inet host` table",
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := newManager().Show(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}
