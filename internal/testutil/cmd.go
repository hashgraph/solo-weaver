// SPDX-License-Identifier: Apache-2.0

package testutil

import "github.com/spf13/cobra"

// PrepareSubCmdForTest creates a root command with the given subcommand added.
// Use this from tests in other packages to avoid duplicating the helper.
func PrepareSubCmdForTest(sub *cobra.Command) *cobra.Command {
	root := &cobra.Command{Use: "root"}
	root.AddCommand(sub)
	return root
}
