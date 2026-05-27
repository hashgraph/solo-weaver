// SPDX-License-Identifier: Apache-2.0

// Package version holds the shared types and helpers used by each binary's
// version subpackage (pkg/version/cli and pkg/version/daemon). The two
// subpackages each embed their own VERSION + COMMIT and register themselves
// here at init time via SetCurrent so shared code (internal/...) can read the
// running binary's version through Get / Number / Commit without knowing which
// binary it belongs to.
package version

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

type Info struct {
	Number    string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	GoVersion string `json:"goversion" yaml:"goversion"`
}

const (
	FormatYAML = "yaml"
	FormatJSON = "json"
)

func (v Info) Format(format string) (string, error) {
	if format == "" {
		return FormatJSON, nil
	}

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

func (v Info) Text() string {
	return fmt.Sprintf("Version: %s\nCommit: %s\nGo Version: %s", v.Number, v.Commit, v.GoVersion)
}

// current holds the running binary's Info, registered at init time by the
// corresponding pkg/version/{cli,daemon} subpackage. Shared code under
// internal/... reads it via Get / Number / Commit.
var current Info

// SetCurrent registers the running binary's Info. Called from each binary's
// pkg/version/{cli,daemon} subpackage init().
func SetCurrent(info Info) {
	current = info
}

// Get returns the running binary's Info.
func Get() Info {
	return current
}

// Number returns the running binary's version number.
func Number() string {
	return current.Number
}

// Commit returns the running binary's commit hash.
func Commit() string {
	return current.Commit
}
