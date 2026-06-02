// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

const fullStateSourcesManifest = `
schemaVersion: v1
stateSources:
  - bucket: "https://storage.googleapis.com/mainnet-state-backups"
    type: gcs
    location: "us-central-1"
    index:
      "council-node-1": "/current-node/council-node-1.txt"
      "council-node-2": "/current-node/council-node-2.txt"
      "council-node-3": "/current-node/council-node-3.txt"
    paths:
      "council-node-1": "/council-node-1"
      "council-node-2": "/council-node-2"
      "council-node-3": "/council-node-3"
  - bucket: "https://s3.cloud.com/mainnet"
    type: s3
    location: "ap-southeast-1"
    index:
      "council-node-11": "/current-node/council-node-11.txt"
      "council-node-12": "/current-node/council-node-12.txt"
    paths:
      "council-node-11": "/council-node-11"
      "council-node-12": "/council-node-12"
`

func TestParseStateSources_FullHappyPath(t *testing.T) {
	doc, err := ParseStateSources([]byte(fullStateSourcesManifest))
	require.NoError(t, err)
	require.Equal(t, SchemaV1, doc.SchemaVersion)
	require.Len(t, doc.Sources, 2)

	require.Equal(t, BucketTypeGCS, doc.Sources[0].Type)
	require.Equal(t, "us-central-1", doc.Sources[0].Location)
	require.Len(t, doc.Sources[0].Index, 3)
	require.Equal(t, "/current-node/council-node-1.txt", doc.Sources[0].Index["council-node-1"])
	require.Equal(t, "/council-node-1", doc.Sources[0].Paths["council-node-1"])

	require.Equal(t, BucketTypeS3, doc.Sources[1].Type)
	require.Len(t, doc.Sources[1].Index, 2)
}

func TestParseStateSources_EmptyStateSourcesTolerated(t *testing.T) {
	// A manifest with no stateSources is a structurally valid no-op — e.g.
	// some networks may not use fast-sync.
	doc, err := ParseStateSources([]byte("schemaVersion: v1\n"))
	require.NoError(t, err)
	require.Empty(t, doc.Sources)
}

func TestParseStateSources_RejectsUnknownTopLevelField(t *testing.T) {
	_, err := ParseStateSources([]byte("schemaVersion: v1\nmysteryField: 1\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseStateSources_RejectsHIPVersionField(t *testing.T) {
	// The HIP draft used `version: v1` at the top level for this manifest;
	// the unified convention across all four manifests is schemaVersion.
	data := []byte(`
version: v1
stateSources: []
`)
	_, err := ParseStateSources(data)
	require.Error(t, err)
	// Missing schemaVersion is caught by the cross-cutting validator (not
	// by strict-decode), so this surfaces as MissingSchemaVersionError —
	// "version: v1" is silently ignored at the schemaVersion stage and the
	// schemaVersion field itself is absent.
	require.True(t, errorx.IsOfType(err, MissingSchemaVersionError),
		"expected MissingSchemaVersionError, got %v", err)
}

func TestParseStateSources_RejectsUnsupportedSchemaVersion(t *testing.T) {
	_, err := ParseStateSources([]byte("schemaVersion: v2\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, UnsupportedSchemaVersionError),
		"expected UnsupportedSchemaVersionError, got %v", err)
}

func TestParseStateSources_RejectsMultipleYAMLDocuments(t *testing.T) {
	data := []byte(`---
schemaVersion: v1
stateSources: []
---
schemaVersion: v1
`)
	_, err := ParseStateSources(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ValidationError),
		"expected ValidationError (extra YAML document), got %v", err)
	require.Contains(t, err.Error(), "exactly one YAML document")
}

func TestParseStateSources_ValidationFailures(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		expectField   string
		expectMessage string
	}{
		{
			name: "missing bucket",
			yaml: `
schemaVersion: v1
stateSources:
  - type: gcs
    location: "us-central-1"
    index: {n1: "/i.txt"}
    paths: {n1: "/p"}
`,
			expectField:   "stateSources[0].bucket",
			expectMessage: "must not be empty",
		},
		{
			name: "missing type",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    location: "us-central-1"
    index: {n1: "/i.txt"}
    paths: {n1: "/p"}
`,
			expectField:   "stateSources[0].type",
			expectMessage: "must not be empty",
		},
		{
			name: "invalid type",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: azure
    location: "us-central-1"
    index: {n1: "/i.txt"}
    paths: {n1: "/p"}
`,
			expectField:   "stateSources[0].type",
			expectMessage: `invalid value "azure"`,
		},
		{
			name: "missing location",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    index: {n1: "/i.txt"}
    paths: {n1: "/p"}
`,
			expectField:   "stateSources[0].location",
			expectMessage: "must not be empty",
		},
		{
			name: "empty index",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    location: "us-central-1"
    index: {}
    paths: {n1: "/p"}
`,
			expectField:   "stateSources[0].index",
			expectMessage: "must declare at least one node",
		},
		{
			name: "empty paths",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    location: "us-central-1"
    index: {n1: "/i.txt"}
    paths: {}
`,
			expectField:   "stateSources[0].paths",
			expectMessage: "must declare at least one node",
		},
		{
			name: "node listed in index but missing from paths",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    location: "us-central-1"
    index: {n1: "/i1", n2: "/i2"}
    paths: {n1: "/p1"}
`,
			expectField:   "stateSources[0].paths",
			expectMessage: `node "n2" is listed in index but missing from paths`,
		},
		{
			name: "node listed in paths but missing from index",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    location: "us-central-1"
    index: {n1: "/i1"}
    paths: {n1: "/p1", n2: "/p2"}
`,
			expectField:   "stateSources[0].index",
			expectMessage: `node "n2" is listed in paths but missing from index`,
		},
		{
			name: "empty value in index",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    location: "us-central-1"
    index: {n1: ""}
    paths: {n1: "/p1"}
`,
			expectField:   "stateSources[0].index.n1",
			expectMessage: "must not be empty",
		},
		{
			name: "duplicate bucket across sources",
			yaml: `
schemaVersion: v1
stateSources:
  - bucket: "gs://x"
    type: gcs
    location: "us-central-1"
    index: {n1: "/i"}
    paths: {n1: "/p"}
  - bucket: "gs://x"
    type: gcs
    location: "us-east-1"
    index: {n2: "/i"}
    paths: {n2: "/p"}
`,
			expectField:   "stateSources[1].bucket",
			expectMessage: `duplicate bucket "gs://x"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStateSources([]byte(tt.yaml))
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

// The same node ID can legitimately appear in multiple buckets — that's the
// whole point of multi-bucket redundancy. Pin that behaviour.
func TestParseStateSources_SameNodeAcrossBucketsAllowed(t *testing.T) {
	data := []byte(`
schemaVersion: v1
stateSources:
  - bucket: "gs://primary"
    type: gcs
    location: "us-central-1"
    index: {n1: "/i", n2: "/i"}
    paths: {n1: "/p", n2: "/p"}
  - bucket: "s3://mirror"
    type: s3
    location: "ap-southeast-1"
    index: {n1: "/i", n2: "/i"}
    paths: {n1: "/p", n2: "/p"}
`)
	_, err := ParseStateSources(data)
	require.NoError(t, err)
}
