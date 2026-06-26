// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestRenderVersion_JSON verifies that the json format (and its case-insensitive
// and empty-default variants) produces valid JSON carrying the build-metadata
// keys exposed by github.com/automa-saga/version. The daemon's pre-cobra
// --version path relies on this json output being a single valid object (parsed
// by internal/workflows/steps' daemonVersionOutput, which reads version+commit).
func TestRenderVersion_JSON(t *testing.T) {
	for _, format := range []string{"", "json", "JSON"} {
		out, err := renderVersion(format)
		require.NoError(t, err, "format %q", format)

		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &m),
			"format %q produced invalid JSON: %s", format, out)
		require.Contains(t, m, "version")
		require.Contains(t, m, "commit")
		require.Contains(t, m, "goVersion")
	}
}

// TestRenderVersion_DefaultMatchesJSON pins the historical default: no --output
// (empty string) renders the same JSON as an explicit "json".
func TestRenderVersion_DefaultMatchesJSON(t *testing.T) {
	empty, err := renderVersion("")
	require.NoError(t, err)
	asJSON, err := renderVersion("json")
	require.NoError(t, err)
	require.Equal(t, asJSON, empty)
}

// TestRenderVersion_YAML verifies the yaml branch emits valid YAML with the
// metadata keys.
func TestRenderVersion_YAML(t *testing.T) {
	for _, format := range []string{"yaml", "YAML"} {
		out, err := renderVersion(format)
		require.NoError(t, err, "format %q", format)

		var m map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(out), &m),
			"format %q produced invalid YAML: %s", format, out)
		require.Contains(t, m, "version")
		require.Contains(t, m, "commit")
		require.Contains(t, m, "goVersion")
	}
}

// TestRenderVersion_UnsupportedFormat asserts unsupported formats are rejected
// with an errorx.IllegalFormat error.
func TestRenderVersion_UnsupportedFormat(t *testing.T) {
	_, err := renderVersion("xml")
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, errorx.IllegalFormat),
		"want errorx.IllegalFormat, got %v", err)
	require.Contains(t, err.Error(), "unsupported format: xml")
}
