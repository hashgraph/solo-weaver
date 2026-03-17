// SPDX-License-Identifier: Apache-2.0

package version

import (
	"github.com/spf13/cobra"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Long:  "Show the current version of the application",
		RunE: func(cmd *cobra.Command, args []string) error {
			// read the inherited --output flag from the merged flag set
			format, _ := cmd.Flags().GetString("output")
			return Print(cmd, format)
		},
	}
)

func Cmd() *cobra.Command {
	return versionCmd
}

func Print(cmd *cobra.Command, format string) error {
	output, err := Get().Format(format)
	if err != nil {
		return err
	}
	cmd.Println(output)
	return nil
}

// Text returns a human-readable version string for display purposes.
func Text() string {
	return Get().Text()
}
