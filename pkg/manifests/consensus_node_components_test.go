// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// fullDeterministicManifest is the happy-path fixture: every component
// present and every layerHashes map populated for both linux/amd64 and
// linux/arm64. The five sidecar uploaders use the deterministic path
// (component-level layerHashes); backupUploader exercises the
// non-deterministic path (per-registry layerHashes, deterministic.supported
// explicitly false) so both code paths see coverage from a single fixture.
const fullDeterministicManifest = `
schemaVersion: v1
images:
  consensusNode:
    enabled: true
    version: "0.75.0"
    deterministic:
      supported: true
      layerHashes:
        linux/arm64:
          - "sha256:cn-arm64-1"
        linux/amd64:
          - "sha256:cn-amd64-1"
    registries:
      - image: "ghcr.io/hashgraph/hedera-services/consensus-node:0.75.0"
      - image: "ghcr.io/other-org/hedera-services/consensus-node:0.75.0"
  recordStreamUploader:
    version: "0.43.0"
    deterministic:
      supported: true
      layerHashes:
        linux/arm64: ["sha256:rsu-arm64"]
        linux/amd64: ["sha256:rsu-amd64"]
    registries:
      - image: "ghcr.io/hashgraph/solo-record-stream:0.43.0"
  eventStreamUploader:
    version: "0.43.0"
    deterministic:
      supported: true
      layerHashes:
        linux/arm64: ["sha256:esu-arm64"]
        linux/amd64: ["sha256:esu-amd64"]
    registries:
      - image: "ghcr.io/hashgraph/solo-event-stream:0.43.0"
  blockStreamUploader:
    version: "0.43.0"
    deterministic:
      supported: true
      layerHashes:
        linux/arm64: ["sha256:bsu-arm64"]
        linux/amd64: ["sha256:bsu-amd64"]
    registries:
      - image: "ghcr.io/hashgraph/solo-block-stream:0.43.0"
  uc:
    version: "1.5.0"
    deterministic:
      supported: true
      layerHashes:
        linux/arm64: ["sha256:uc-arm64"]
        linux/amd64: ["sha256:uc-amd64"]
    registries:
      - image: "ghcr.io/hashgraph/solo-uc:1.5.0"
  backupUploader:
    enabled: false
    version: "0.33.0"
    deterministic:
      supported: false
    registries:
      - image: "ghcr.io/hashgraph/hedera-services/backup-uploader:0.33.0"
        layerHashes:
          linux/arm64: ["sha256:bu-ghcr-arm64"]
          linux/amd64: ["sha256:bu-ghcr-amd64"]
      - image: "ghcr.io/other-org/hedera-services/backup-uploader:0.33.0"
        layerHashes:
          linux/arm64: ["sha256:bu-other-arm64"]
          linux/amd64: ["sha256:bu-other-amd64"]
`

func TestParseConsensusNodeComponents_FullHappyPath(t *testing.T) {
	doc, err := ParseConsensusNodeComponents([]byte(fullDeterministicManifest))
	require.NoError(t, err)
	require.Equal(t, SchemaV1, doc.SchemaVersion)

	require.NotNil(t, doc.Images.ConsensusNode)
	require.Equal(t, "0.75.0", doc.Images.ConsensusNode.Version)
	require.NotNil(t, doc.Images.ConsensusNode.Enabled)
	require.True(t, *doc.Images.ConsensusNode.Enabled)
	require.Len(t, doc.Images.ConsensusNode.Registries, 2)

	require.NotNil(t, doc.Images.BackupUploader)
	require.NotNil(t, doc.Images.BackupUploader.Enabled)
	require.False(t, *doc.Images.BackupUploader.Enabled)
	require.False(t, doc.Images.BackupUploader.Deterministic.Supported)
	require.Empty(t, doc.Images.BackupUploader.Deterministic.LayerHashes)
	require.Len(t, doc.Images.BackupUploader.Registries[0].LayerHashes, 2)
}

func TestParseConsensusNodeComponents_AbsentSectionsTolerated(t *testing.T) {
	// "Absent = no change" — only consensusNode declared; the other five
	// components are simply not in the manifest, which is valid.
	data := []byte(`
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic:
      supported: true
      layerHashes:
        linux/arm64: ["sha256:a"]
        linux/amd64: ["sha256:b"]
    registries:
      - image: "ghcr.io/x:0.75.0"
`)
	doc, err := ParseConsensusNodeComponents(data)
	require.NoError(t, err)
	require.NotNil(t, doc.Images.ConsensusNode)
	require.Nil(t, doc.Images.RecordStreamUploader)
	require.Nil(t, doc.Images.UC)
	require.Nil(t, doc.Images.ConsensusNode.Enabled, "absent enabled field stays nil")
}

func TestParseConsensusNodeComponents_EmptyImagesTolerated(t *testing.T) {
	// images: {} (or images: ) — the manifest changes nothing. Permitted.
	doc, err := ParseConsensusNodeComponents([]byte("schemaVersion: v1\nimages: {}\n"))
	require.NoError(t, err)
	require.Nil(t, doc.Images.ConsensusNode)
}

func TestParseConsensusNodeComponents_RejectsUnknownTopLevelField(t *testing.T) {
	data := []byte(`
schemaVersion: v1
mysteryField: 42
images: {}
`)
	_, err := ParseConsensusNodeComponents(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseConsensusNodeComponents_RejectsUnknownComponentName(t *testing.T) {
	// "cheetah" was the bundled-sidecar name in the HIP draft but the story
	// for #531 split it into five named uploaders. Unknown component names
	// must surface as a parse error rather than be silently dropped.
	data := []byte(`
schemaVersion: v1
images:
  cheetah:
    version: "0.43.0"
    deterministic: {supported: true, layerHashes: {linux/amd64: ["sha256:x"], linux/arm64: ["sha256:y"]}}
    registries: [{image: "ghcr.io/x:0.43.0"}]
`)
	_, err := ParseConsensusNodeComponents(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseConsensusNodeComponents_RejectsUnsupportedSchemaVersion(t *testing.T) {
	// The schemaVersion gate fires before any consensus-node-specific decode.
	_, err := ParseConsensusNodeComponents([]byte("schemaVersion: v2\nimages: {}\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, UnsupportedSchemaVersionError),
		"expected UnsupportedSchemaVersionError, got %v", err)
}

func TestParseConsensusNodeComponents_ValidationFailures(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		expectField   string
		expectMessage string
	}{
		{
			name: "missing version",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    deterministic: {supported: true, layerHashes: {linux/amd64: ["x"], linux/arm64: ["y"]}}
    registries: [{image: "ghcr.io/x:0.75.0"}]
`,
			expectField:   "images.consensusNode.version",
			expectMessage: "must not be empty",
		},
		{
			name: "empty registries",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic: {supported: true, layerHashes: {linux/amd64: ["x"], linux/arm64: ["y"]}}
    registries: []
`,
			expectField:   "images.consensusNode.registries",
			expectMessage: "must declare at least one registry",
		},
		{
			name: "registry missing image",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic: {supported: true, layerHashes: {linux/amd64: ["x"], linux/arm64: ["y"]}}
    registries:
      - image: ""
`,
			expectField:   "images.consensusNode.registries[0].image",
			expectMessage: "must not be empty",
		},
		{
			name: "deterministic supported but no layerHashes",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic: {supported: true}
    registries: [{image: "ghcr.io/x:0.75.0"}]
`,
			expectField:   "images.consensusNode.deterministic.layerHashes",
			expectMessage: "must be declared when deterministic.supported is true",
		},
		{
			name: "deterministic supported but registry has override",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic: {supported: true, layerHashes: {linux/amd64: ["x"], linux/arm64: ["y"]}}
    registries:
      - image: "ghcr.io/x:0.75.0"
        layerHashes:
          linux/amd64: ["sha256:rogue"]
`,
			expectField:   "images.consensusNode.registries[0].layerHashes",
			expectMessage: "must not be set when deterministic.supported is true",
		},
		{
			name: "deterministic unsupported but layerHashes set at deterministic level",
			yaml: `
schemaVersion: v1
images:
  backupUploader:
    version: "0.33.0"
    deterministic:
      supported: false
      layerHashes: {linux/amd64: ["x"]}
    registries:
      - image: "ghcr.io/x:0.33.0"
        layerHashes: {linux/amd64: ["y"], linux/arm64: ["z"]}
`,
			expectField:   "images.backupUploader.deterministic.layerHashes",
			expectMessage: "must not be set when deterministic.supported is false",
		},
		{
			name: "deterministic absent — registry must carry layerHashes",
			yaml: `
schemaVersion: v1
images:
  backupUploader:
    version: "0.33.0"
    registries:
      - image: "ghcr.io/x:0.33.0"
`,
			expectField:   "images.backupUploader.registries[0].layerHashes",
			expectMessage: "must be declared for non-deterministic components",
		},
		{
			name: "unsupported platform key",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic:
      supported: true
      layerHashes:
        linux/amd64: ["x"]
        linux/arm64: ["y"]
        windows/arm64: ["z"]
    registries:
      - image: "ghcr.io/x:0.75.0"
`,
			expectField:   "images.consensusNode.deterministic.layerHashes",
			expectMessage: `unsupported platform "windows/arm64"`,
		},
		{
			name: "empty layer hash list for a platform",
			yaml: `
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic:
      supported: true
      layerHashes:
        linux/amd64: ["x"]
        linux/arm64: []
    registries:
      - image: "ghcr.io/x:0.75.0"
`,
			expectField:   "images.consensusNode.deterministic.layerHashes.linux/arm64",
			expectMessage: "must declare at least one layer hash",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConsensusNodeComponents([]byte(tt.yaml))
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

func TestParseConsensusNodeComponents_MalformedYAML(t *testing.T) {
	_, err := ParseConsensusNodeComponents([]byte("schemaVersion: v1\nimages: [not, a, map]\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

// Pins the contract that the parser refuses inputs containing more than one
// YAML document. Without this check yaml.Decoder.Decode would consume only
// the first document and silently drop the rest — a failure mode that's
// easy to ship and hard to detect for a council-signed manifest.
func TestParseConsensusNodeComponents_RejectsMultipleYAMLDocuments(t *testing.T) {
	data := []byte(`---
schemaVersion: v1
images:
  consensusNode:
    version: "0.75.0"
    deterministic:
      supported: true
      layerHashes:
        linux/amd64: ["sha256:a"]
        linux/arm64: ["sha256:b"]
    registries:
      - image: "ghcr.io/x:0.75.0"
---
schemaVersion: v1
images: {}
`)
	_, err := ParseConsensusNodeComponents(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ValidationError),
		"expected ValidationError (extra YAML document), got %v", err)
	require.Contains(t, err.Error(), "exactly one YAML document")
}
