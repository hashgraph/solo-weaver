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

// Pins the contract that this parser does NOT enforce the specific allowed
// destination marker prefixes (HAPIAPP_DIR, SOLO_PROVISIONER_DIR). That
// strict-allowlist enforcement is the subject of #536, which is the
// follow-on PR in this epic. Until then, any non-empty destination passes.
func TestParseExternalFiles_DestinationPrefixNotYetEnforced(t *testing.T) {
	data := []byte(`
schemaVersion: v1
files:
  - url: "s3://x/y"
    algorithm: sha256
    checksum: "z"
    destination: "/absolute/path/elsewhere.bin"
    phase: {download: prepare, install: freeze}
`)
	_, err := ParseExternalFiles(data)
	require.NoError(t, err, "destination allowlist enforcement is deferred to #536")
}
