package versioncmd

import (
	"encoding/json"
	"strings"

	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/doctor"
	"golang.hedera.com/solo-weaver/internal/version"
	"gopkg.in/yaml.v3"
)

type VersionInfo struct {
	Number string `json:"version" yaml:"version"`
	Commit string `json:"commit" yaml:"commit"`
}

func (v *VersionInfo) Format(format string) (string, error) {
	var output []byte
	var err error
	switch strings.ToLower(format) {
	case "json":
		output, err = json.Marshal(versionInfo)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "Error marshaling version info to JSON")
		}
	case "yaml":
		output, err = yaml.Marshal(versionInfo)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "Error marshaling version info to YAML")
		}
	default:
		return "", errorx.IllegalFormat.New("unsupported format: %s", format)
	}

	return string(output), nil
}

var (
	versionInfo *VersionInfo

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
	versionInfo = &VersionInfo{
		Number: version.Number(),
		Commit: version.Commit(),
	}
	versionCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "yaml", "Output format: yaml|json")
}

func Get() *cobra.Command {
	return versionCmd
}

func Version() *VersionInfo {
	return &VersionInfo{
		Number: versionInfo.Number,
		Commit: versionInfo.Commit,
	}
}

func PrintVersion(cmd *cobra.Command, format string) {
	output, err := versionInfo.Format(format)
	if err != nil {
		doctor.CheckErr(cmd.Context(), err)
	}
	cmd.Println(output)
}
