// SPDX-License-Identifier: Apache-2.0

package version

import (
	"encoding/json"
	"runtime"
	"strings"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

type Info struct {
	Number    string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	GoVersion string `json:"go" yaml:"go"`
}

const (
	FormatYAML = "yaml"
	FormatJSON = "json"
)

func (v Info) Format(format string) (string, error) {
	var output []byte
	var err error
	switch strings.ToLower(format) {
	case FormatJSON:
		output, err = json.Marshal(v)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "Error marshaling version info to JSON")
		}
	case FormatYAML:
		output, err = yaml.Marshal(v)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "Error marshaling version info to YAML")
		}
	default:
		return "", errorx.IllegalFormat.New("unsupported format: %s", format)
	}

	return string(output), nil
}

var (
	versionInfo Info
)

func init() {
	versionInfo = Info{
		Number:    Number(),
		Commit:    Commit(),
		GoVersion: runtime.Version(),
	}
}

func Get() Info {
	return versionInfo
}
