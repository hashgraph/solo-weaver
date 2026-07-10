// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"strings"

	"github.com/automa-saga/version"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// renderVersion renders the running binary's build metadata (stamped at build
// time via -ldflags -X into github.com/automa-saga/version) in the requested
// output format. Text is the default; "json" produces machine-readable JSON.
// These mirror the global --output values (text, json).
func renderVersion(format string) (string, error) {
	info := version.Get()
	switch strings.ToLower(format) {
	case version.FormatJSON:
		return info.Format(version.FormatJSON)
	case "", version.FormatText:
		return info.Format(version.FormatText)
	default:
		return "", errorx.IllegalFormat.New("unsupported format %q (want text or json)", format)
	}
}

// newVersionCmd returns the "version" Cobra command. It reads the inherited
// --output flag and prints the running binary's version metadata.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Long:  "Show the current version of the application",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("output")
			out, err := renderVersion(format)
			if err != nil {
				return err
			}
			cmd.Println(out)
			return nil
		},
	}
}
