// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

const fullExternalFilesManifest = `
schemaVersion: v1
files:
  - url: "s3://hedera-artifacts/keys/genesis-key.bin"
    algorithm: sha256
    checksum: "abc123"
    contentType: "application/octet-stream"
    destination: "HAPIAPP_DIR/data/keys/genesis-key.bin"
    phase:
      download: prepare
      install: freeze
  - url: "https://example.org/cert.pem"
    algorithm: sha256
    checksum: "def456"
    destination: "SOLO_PROVISIONER_DIR/certs/cert.pem"
    optional: true
    phase:
      download: freeze
      install: freeze
`

func TestParseExternalFiles_FullHappyPath(t *testing.T) {
	doc, err := ParseExternalFiles([]byte(fullExternalFilesManifest))
	require.NoError(t, err)
	require.Equal(t, SchemaV1, doc.SchemaVersion)
	require.Len(t, doc.Files, 2)

	require.Equal(t, "s3://hedera-artifacts/keys/genesis-key.bin", doc.Files[0].URL)
	require.Equal(t, "sha256", doc.Files[0].Algorithm)
	require.Equal(t, "application/octet-stream", doc.Files[0].ContentType)
	require.False(t, doc.Files[0].Optional, "optional defaults to false when absent")
	require.Equal(t, DownloadPhasePrepare, doc.Files[0].Phase.Download)
	require.Equal(t, InstallPhaseFreeze, doc.Files[0].Phase.Install)

	require.True(t, doc.Files[1].Optional)
	require.Equal(t, DownloadPhaseFreeze, doc.Files[1].Phase.Download)
}

func TestParseExternalFiles_EmptyFilesList(t *testing.T) {
	// A manifest with no files declared is a structurally valid no-op.
	doc, err := ParseExternalFiles([]byte("schemaVersion: v1\n"))
	require.NoError(t, err)
	require.Empty(t, doc.Files)
}

func TestParseExternalFiles_OptionalDefaultsFalse(t *testing.T) {
	// The story specifies optional defaults to false. Verify yaml.v3 produces
	// that result for an entry that omits optional entirely.
	data := []byte(`
schemaVersion: v1
files:
  - url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase:
      download: prepare
      install: freeze
`)
	doc, err := ParseExternalFiles(data)
	require.NoError(t, err)
	require.False(t, doc.Files[0].Optional)
}

func TestParseExternalFiles_RejectsUnknownTopLevelField(t *testing.T) {
	_, err := ParseExternalFiles([]byte("schemaVersion: v1\nmysteryField: 1\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseExternalFiles_RejectsHIPHashFieldShape(t *testing.T) {
	// The HIP draft used a single hash: "sha256:..." field; the story body
	// for #533 supersedes that with separate algorithm + checksum fields.
	// A manifest using the old shape must surface as a parse error.
	data := []byte(`
schemaVersion: v1
files:
  - url: "s3://x/y"
    hash: "sha256:abc"
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare, install: freeze}
`)
	_, err := ParseExternalFiles(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseExternalFiles_RejectsUnsupportedSchemaVersion(t *testing.T) {
	_, err := ParseExternalFiles([]byte("schemaVersion: v2\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, UnsupportedSchemaVersionError),
		"expected UnsupportedSchemaVersionError, got %v", err)
}

func TestParseExternalFiles_ValidationFailures(t *testing.T) {
	base := func(over string) string {
		// Build a minimal valid entry and overlay the specific failure shape.
		return "schemaVersion: v1\nfiles:\n  - " + strings.TrimPrefix(over, "- ")
	}

	tests := []struct {
		name          string
		yaml          string
		expectField   string
		expectMessage string
	}{
		{
			name: "missing url",
			yaml: base(`- algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare, install: freeze}`),
			expectField:   "files[0].url",
			expectMessage: "must not be empty",
		},
		{
			name: "missing algorithm",
			yaml: base(`- url: "s3://x/y"
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare, install: freeze}`),
			expectField:   "files[0].algorithm",
			expectMessage: "must not be empty",
		},
		{
			name: "missing checksum",
			yaml: base(`- url: "s3://x/y"
    algorithm: sha256
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare, install: freeze}`),
			expectField:   "files[0].checksum",
			expectMessage: "must not be empty",
		},
		{
			name: "missing destination",
			yaml: base(`- url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    phase: {download: prepare, install: freeze}`),
			expectField:   "files[0].destination",
			expectMessage: "must not be empty",
		},
		{
			name: "phase.download missing",
			yaml: base(`- url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {install: freeze}`),
			expectField:   "files[0].phase.download",
			expectMessage: "must not be empty",
		},
		{
			name: "phase.download invalid value",
			yaml: base(`- url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {download: install, install: freeze}`),
			expectField:   "files[0].phase.download",
			expectMessage: `invalid value "install"`,
		},
		{
			name: "phase.install missing",
			yaml: base(`- url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare}`),
			expectField:   "files[0].phase.install",
			expectMessage: "must not be empty",
		},
		{
			name: "phase.install non-freeze value",
			yaml: base(`- url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare, install: prepare}`),
			expectField:   "files[0].phase.install",
			expectMessage: `invalid value "prepare"`,
		},
		{
			name: "duplicate destinations",
			yaml: `
schemaVersion: v1
files:
  - url: "s3://x/a"
    algorithm: sha256
    checksum: "1"
    destination: "HAPIAPP_DIR/a"
    phase: {download: prepare, install: freeze}
  - url: "s3://y/a"
    algorithm: sha256
    checksum: "2"
    destination: "HAPIAPP_DIR/a"
    phase: {download: prepare, install: freeze}
`,
			expectField:   "files[1].destination",
			expectMessage: `duplicate destination "HAPIAPP_DIR/a"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExternalFiles([]byte(tt.yaml))
			require.Error(t, err)
			require.True(t, errorx.IsOfType(err, ValidationError),
				"expected ValidationError, got %v", err)
			msg := err.Error()
			require.Contains(t, msg, tt.expectField, "error should reference field path")
			require.True(t, strings.Contains(msg, tt.expectMessage),
				"expected message to contain %q, got %q", tt.expectMessage, msg)
		})
	}
}

func TestParseExternalFiles_RejectsMultipleYAMLDocuments(t *testing.T) {
	data := []byte(`---
schemaVersion: v1
files:
  - url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "HAPIAPP_DIR/y"
    phase: {download: prepare, install: freeze}
---
schemaVersion: v1
files: []
`)
	_, err := ParseExternalFiles(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ValidationError),
		"expected ValidationError (extra YAML document), got %v", err)
	require.Contains(t, err.Error(), "exactly one YAML document")
}

// Pins the closed-set policy on destination prefixes. Both recognised
// markers are accepted; anything else — absolute paths, relative paths,
// unknown markers, near-misses — fails parsing.
func TestParseExternalFiles_DestinationPrefixEnforcement(t *testing.T) {
	acceptCases := []struct {
		name        string
		destination string
	}{
		{"HAPIAPP_DIR with subpath", "HAPIAPP_DIR/data/keys/a.bin"},
		{"SOLO_PROVISIONER_DIR with subpath", "SOLO_PROVISIONER_DIR/certs/a.pem"},
		// "Is this a sensible path under HAPIAPP_DIR?" is the downloader's
		// responsibility, not the parser's. We only enforce the marker.
		{"marker with empty subpath", "HAPIAPP_DIR/"},
	}
	for _, tc := range acceptCases {
		t.Run("accepts: "+tc.name, func(t *testing.T) {
			data := []byte(`
schemaVersion: v1
files:
  - url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "` + tc.destination + `"
    phase: {download: prepare, install: freeze}
`)
			_, err := ParseExternalFiles(data)
			require.NoError(t, err)
		})
	}

	rejectCases := []struct {
		name        string
		destination string
	}{
		{"absolute filesystem path", "/absolute/path/elsewhere.bin"},
		{"relative path", "data/keys/a.bin"},
		{"unknown marker", "HEDERA_DATA_DIR/data/keys/a.bin"},
		{"marker without trailing slash", "HAPIAPP_DIR"},
		{"marker as prefix of longer token", "HAPIAPP_DIR_FOO/x"},
	}
	for _, tc := range rejectCases {
		t.Run("rejects: "+tc.name, func(t *testing.T) {
			data := []byte(`
schemaVersion: v1
files:
  - url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "` + tc.destination + `"
    phase: {download: prepare, install: freeze}
`)
			_, err := ParseExternalFiles(data)
			require.Error(t, err)
			require.True(t, errorx.IsOfType(err, ValidationError),
				"expected ValidationError, got %v", err)
			require.Contains(t, err.Error(), "files[0].destination")
			require.Contains(t, err.Error(), "marker prefix")
			require.Contains(t, err.Error(), "HAPIAPP_DIR")
			require.Contains(t, err.Error(), "SOLO_PROVISIONER_DIR")
		})
	}
}

// AllowedDestinationPrefixes is part of the public API; pin its exact
// contents so a future widening (or narrowing) is a deliberate decision
// visible in the diff.
func TestAllowedDestinationPrefixes_PinnedSet(t *testing.T) {
	require.Equal(t, []string{"HAPIAPP_DIR", "SOLO_PROVISIONER_DIR"}, AllowedDestinationPrefixes())
}

// Mutating the slice returned by AllowedDestinationPrefixes must not weaken
// or alter enforcement — the exported accessor returns a clone so callers
// cannot reach the internal source of truth used by validateDestinationPrefix.
func TestAllowedDestinationPrefixes_ReturnedSliceIsClone(t *testing.T) {
	got := AllowedDestinationPrefixes()
	for i := range got {
		got[i] = "MUTATED"
	}
	require.Equal(t, []string{"HAPIAPP_DIR", "SOLO_PROVISIONER_DIR"}, AllowedDestinationPrefixes(),
		"mutating the returned slice must not affect subsequent calls")

	err := validateDestinationPrefix("files[0].destination", "MUTATED/foo")
	require.Error(t, err, "enforcement must reject a value that only matched the caller-mutated slice")
}
