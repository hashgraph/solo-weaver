// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"

	"github.com/automa-saga/version"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// renderVersion renders the daemon's build metadata (stamped at build time via
// -ldflags -X into github.com/automa-saga/version) in the requested output
// format. JSON is the historical default. YAML is produced by marshaling the
// yaml-tagged Info directly, as documented by automa-saga/version (whose Format
// only marshals json/text to stay dependency-free).
func renderVersion(format string) (string, error) {
	info := version.Get()
	switch strings.ToLower(format) {
	case "", version.FormatJSON:
		return info.Format(version.FormatJSON)
	case "yaml":
		out, err := yaml.Marshal(info)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "Error marshaling version info to YAML")
		}
		return string(out), nil
	default:
		return "", errorx.IllegalFormat.New("unsupported format: %s", format)
	}
}

// newVersionCmd returns the "version" Cobra command. It reads the inherited
// --output flag and prints the daemon's version metadata.
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
