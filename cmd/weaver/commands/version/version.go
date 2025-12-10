// SPDX-License-Identifier: Apache-2.0

package version

import (
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/version"
	"github.com/spf13/cobra"
)

var (
	flagOutputFormat string

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Long:  "Show the current version of the application",
		Run: func(cmd *cobra.Command, args []string) {
			PrintVersion(cmd, flagOutputFormat)
		},
	}
)

func init() {
	versionCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "yaml", "Output format: yaml|json")
}

func GetCmd() *cobra.Command {
	return versionCmd
}

func PrintVersion(cmd *cobra.Command, format string) {
	output, err := version.Get().Format(format)
	if err != nil {
		doctor.CheckErr(cmd.Context(), err)
	}
	cmd.Println(output)
}
