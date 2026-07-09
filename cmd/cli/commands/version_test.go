// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// TestRenderVersion_JSON verifies that the json format (case-insensitively)
// produces valid JSON carrying the build-metadata keys exposed by
// github.com/automa-saga/version. Values are not asserted because they are
// stamped via -ldflags at build time and default to dev/none under `go test`.
func TestRenderVersion_JSON(t *testing.T) {
	for _, format := range []string{"json", "JSON"} {
		out, err := renderVersion(format)
		require.NoError(t, err, "format %q", format)

		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &m),
			"format %q produced invalid JSON: %s", format, out)
		require.Contains(t, m, "version")
		require.Contains(t, m, "commit")
		require.Contains(t, m, "goVersion") // upstream renamed key (was "goversion")
	}
}

// TestRenderVersion_DefaultIsText pins the new default: no --output (empty
// string) renders human-readable text, identical to an explicit "text" and
// distinct from JSON.
func TestRenderVersion_DefaultIsText(t *testing.T) {
	empty, err := renderVersion("")
	require.NoError(t, err)
	require.NotEmpty(t, empty)

	asText, err := renderVersion("text")
	require.NoError(t, err)
	require.Equal(t, asText, empty)

	// Text output is not JSON.
	var m map[string]any
	require.Error(t, json.Unmarshal([]byte(empty), &m),
		"default output should be text, not JSON: %s", empty)
}

// TestRenderVersion_UnsupportedFormat asserts unsupported formats (including the
// now-removed "yaml") are rejected with an errorx.IllegalFormat error naming the
// supported set.
func TestRenderVersion_UnsupportedFormat(t *testing.T) {
	for _, format := range []string{"yaml", "xml"} {
		_, err := renderVersion(format)
		require.Error(t, err, "format %q", format)
		require.True(t, errorx.IsOfType(err, errorx.IllegalFormat),
			"want errorx.IllegalFormat, got %v", err)
		require.Contains(t, err.Error(), "want text or json")
	}
}
