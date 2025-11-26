package version

import (
	"encoding/json"
	"runtime"
	"strings"

	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/doctor"
	"golang.hedera.com/solo-weaver/internal/version"
	"gopkg.in/yaml.v3"
)

type VersionInfo struct {
	Number    string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	GoVersion string `json:"go" yaml:"go"`
}

const (
	FormatYAML = "yaml"
	FormatJSON = "json"
)

func (v *VersionInfo) Format(format string) (string, error) {
	var output []byte
	var err error
	switch strings.ToLower(format) {
	case FormatJSON:
		output, err = json.Marshal(versionInfo)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "Error marshaling version info to JSON")
		}
	case FormatYAML:
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
		Number:    version.Number(),
		Commit:    version.Commit(),
		GoVersion: runtime.Version(),
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
