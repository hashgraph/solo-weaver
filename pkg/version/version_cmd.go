// SPDX-License-Identifier: Apache-2.0

package version

import (
	"github.com/spf13/cobra"
)

// NewCmd returns a "version" Cobra command that prints the Info returned by
// getter. Each binary's version subpackage instantiates this with its own
// Get function so the parent package stays free of binary-specific state.
func NewCmd(getter func() Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Long:  "Show the current version of the application",
		RunE: func(cmd *cobra.Command, args []string) error {
			// read the inherited --output flag from the merged flag set
			format, _ := cmd.Flags().GetString("output")
			return Print(cmd, format, getter())
		},
	}
}

// Print writes info in the requested format to cmd's stdout.
func Print(cmd *cobra.Command, format string, info Info) error {
	output, err := info.Format(format)
	if err != nil {
		return err
	}
	cmd.Println(output)
	return nil
}

// Text returns a human-readable version string for display purposes.
func Text(info Info) string {
	return info.Text()
}
