// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// fullInfrastructureVersionsManifest is the happy-path fixture: both
// provisioner binaries with full integrity records, plus a representative
// host[] and cluster[] audit list.
const fullInfrastructureVersionsManifest = `
schemaVersion: 1
provisioner:
  cli:
    version: "0.42.0"
    algorithm: sha256
    checksum: "abc123"
  daemon:
    version: "0.42.0"
    algorithm: sha256
    checksum: "def456"
host:
  - name: cri-o
    version: "1.33.4"
  - name: kubelet
    version: "1.33.4"
  - name: kubeadm
    version: "1.33.4"
  - name: kubectl
    version: "1.33.4"
  - name: helm
    version: "3.18.6"
  - name: cilium
    version: "0.18.7"
cluster:
  - name: alloy
    version: "1.4.0"
  - name: metallb
    version: "0.15.2"
  - name: metrics-server
    version: "3.13.0"
`

func TestParseInfrastructureVersions_FullHappyPath(t *testing.T) {
	doc, err := ParseInfrastructureVersions([]byte(fullInfrastructureVersionsManifest))
	require.NoError(t, err)
	require.Equal(t, SchemaV1, doc.SchemaVersion)

	require.NotNil(t, doc.Provisioner)
	require.NotNil(t, doc.Provisioner.CLI)
	require.Equal(t, "0.42.0", doc.Provisioner.CLI.Version)
	require.Equal(t, "sha256", doc.Provisioner.CLI.Algorithm)
	require.Equal(t, "abc123", doc.Provisioner.CLI.Checksum)
	require.NotNil(t, doc.Provisioner.Daemon)
	require.Equal(t, "def456", doc.Provisioner.Daemon.Checksum)

	require.Len(t, doc.Host, 6)
	require.Equal(t, "cri-o", doc.Host[0].Name)
	require.Equal(t, "1.33.4", doc.Host[0].Version)

	require.Len(t, doc.Cluster, 3)
	require.Equal(t, "alloy", doc.Cluster[0].Name)
}

func TestParseInfrastructureVersions_AllSectionsAbsent(t *testing.T) {
	// Per "absent = no change", a manifest with only schemaVersion is a no-op
	// but is structurally valid.
	doc, err := ParseInfrastructureVersions([]byte("schemaVersion: 1\n"))
	require.NoError(t, err)
	require.Nil(t, doc.Provisioner)
	require.Empty(t, doc.Host)
	require.Empty(t, doc.Cluster)
}

func TestParseInfrastructureVersions_OnlyCLIPresent(t *testing.T) {
	// "Absent = no change" applies inside provisioner too: declaring only the
	// CLI's record (e.g. a CLI-only release) is valid.
	data := []byte(`
schemaVersion: 1
provisioner:
  cli:
    version: "0.42.0"
    algorithm: sha256
    checksum: "abc"
`)
	doc, err := ParseInfrastructureVersions(data)
	require.NoError(t, err)
	require.NotNil(t, doc.Provisioner)
	require.NotNil(t, doc.Provisioner.CLI)
	require.Nil(t, doc.Provisioner.Daemon)
}

func TestParseInfrastructureVersions_RejectsUnknownTopLevelField(t *testing.T) {
	_, err := ParseInfrastructureVersions([]byte("schemaVersion: 1\nmysteryField: 42\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseInfrastructureVersions_RejectsHIPSingleProvisionerVersion(t *testing.T) {
	// The HIP draft used `provisioner.version`; the story body for #532
	// supersedes it with the cli/daemon split. A manifest written against
	// the old shape must surface as a parse error rather than be silently
	// accepted with both cli and daemon nil.
	data := []byte(`
schemaVersion: 1
provisioner:
  version: "0.42.0"
`)
	_, err := ParseInfrastructureVersions(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError), "expected ParseError, got %v", err)
}

func TestParseInfrastructureVersions_RejectsUnsupportedSchemaVersion(t *testing.T) {
	_, err := ParseInfrastructureVersions([]byte("schemaVersion: 2\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, UnsupportedSchemaVersionError),
		"expected UnsupportedSchemaVersionError, got %v", err)
}

func TestParseInfrastructureVersions_ValidationFailures(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		expectField   string
		expectMessage string
	}{
		{
			name: "cli missing version",
			yaml: `
schemaVersion: 1
provisioner:
  cli:
    algorithm: sha256
    checksum: "abc"
`,
			expectField:   "provisioner.cli.version",
			expectMessage: "must not be empty",
		},
		{
			name: "cli missing algorithm",
			yaml: `
schemaVersion: 1
provisioner:
  cli:
    version: "0.42.0"
    checksum: "abc"
`,
			expectField:   "provisioner.cli.algorithm",
			expectMessage: "must not be empty",
		},
		{
			name: "daemon missing checksum",
			yaml: `
schemaVersion: 1
provisioner:
  daemon:
    version: "0.42.0"
    algorithm: sha256
`,
			expectField:   "provisioner.daemon.checksum",
			expectMessage: "must not be empty",
		},
		{
			name: "host entry missing name",
			yaml: `
schemaVersion: 1
host:
  - name: cri-o
    version: "1.33.4"
  - version: "1.33.4"
`,
			expectField:   "host[1].name",
			expectMessage: "must not be empty",
		},
		{
			name: "host entry missing version",
			yaml: `
schemaVersion: 1
host:
  - name: cri-o
`,
			expectField:   "host[0].version",
			expectMessage: "must not be empty",
		},
		{
			name: "duplicate host names",
			yaml: `
schemaVersion: 1
host:
  - name: cri-o
    version: "1.33.4"
  - name: cri-o
    version: "1.33.5"
`,
			expectField:   "host[1].name",
			expectMessage: `duplicate entry "cri-o"`,
		},
		{
			name: "cluster entry missing version",
			yaml: `
schemaVersion: 1
cluster:
  - name: alloy
`,
			expectField:   "cluster[0].version",
			expectMessage: "must not be empty",
		},
		{
			name: "duplicate cluster names",
			yaml: `
schemaVersion: 1
cluster:
  - name: alloy
    version: "1.4.0"
  - name: alloy
    version: "1.4.1"
`,
			expectField:   "cluster[1].name",
			expectMessage: `duplicate entry "alloy"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseInfrastructureVersions([]byte(tt.yaml))
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

func TestParseInfrastructureVersions_RejectsMultipleYAMLDocuments(t *testing.T) {
	data := []byte(`---
schemaVersion: 1
host:
  - name: cri-o
    version: "1.33.4"
---
schemaVersion: 1
`)
	_, err := ParseInfrastructureVersions(data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ValidationError),
		"expected ValidationError (extra YAML document), got %v", err)
	require.Contains(t, err.Error(), "exactly one YAML document")
}

// Host and cluster names are independent namespaces — the same name appearing
// in both sections (e.g. some-tool packaged both ways) is allowed.
func TestParseInfrastructureVersions_NamesShareableAcrossSections(t *testing.T) {
	data := []byte(`
schemaVersion: 1
host:
  - name: helm
    version: "3.18.6"
cluster:
  - name: helm
    version: "0.0.0"
`)
	_, err := ParseInfrastructureVersions(data)
	require.NoError(t, err)
}
