// SPDX-License-Identifier: Apache-2.0

package software

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/deps"
)

func catalogFiles(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

const testHostTool = `
host:
- name: tool
  versions:
    1.0.0:
      binaries:
        - name: 'tool'
          url: 'https://example.com/tool'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: 'aa'
`

const testHostK8sOld = `
host:
- name: kubectl
  versions:
    1.33.4:
      binaries:
        - name: 'kubectl'
          url: 'https://example.com/kubectl'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: 'bb'
`

const testHostK8sNew = `
host:
- name: kubectl
  versions:
    1.36.5:
      binaries:
        - name: 'kubectl'
          url: 'https://example.com/kubectl'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: 'cc'
`

const testCluster = `
cluster:
- name: metallb
  type: classic
  repo: 'https://metallb.github.io/metallb'
  chart: 'metallb/metallb'
  namespace: 'metallb-system'
  release: 'metallb'
  versions:
    0.16.1:
      algorithm: 'sha256'
      checksum: 'dd'
`

const testDefaults = `
host:
  kubectl: "1.36.5"
  tool: "1.0.0"
cluster:
  metallb: "0.16.1"
`

func validCatalogFiles() map[string]string {
	return map[string]string{
		"defaults.yaml":      testDefaults,
		"host-k8s-1.33.yaml": testHostK8sOld,
		"host-k8s-1.36.yaml": testHostK8sNew,
		"host-tools.yaml":    testHostTool,
		"cluster.yaml":       testCluster,
	}
}

func TestLoadCatalog_MergesFiles(t *testing.T) {
	catalog, err := loadCatalog(catalogFiles(validCatalogFiles()))
	require.NoError(t, err)

	// Files are read in lexical order; a component sits where it first appears.
	assert.Equal(t, []string{"kubectl", "tool"}, catalog.HostNames())
	assert.Equal(t, []string{"metallb"}, catalog.ClusterNames())

	kubectl, err := catalog.GetHostArtifact("kubectl")
	require.NoError(t, err)
	assert.Equal(t, Version("1.36.5"), kubectl.Default)
	assert.Contains(t, kubectl.Versions, Version("1.33.4"))
	assert.Contains(t, kubectl.Versions, Version("1.36.5"))

	metallb, err := catalog.GetClusterComponent("metallb")
	require.NoError(t, err)
	assert.Equal(t, Version("0.16.1"), metallb.Default)
}

func TestLoadCatalog_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(files map[string]string)
		wantErr string
	}{
		{
			name:    "missing defaults file",
			mutate:  func(f map[string]string) { delete(f, "defaults.yaml") },
			wantErr: "catalog has no defaults.yaml",
		},
		{
			name:    "same version in two files",
			mutate:  func(f map[string]string) { f["host-k8s-1.36.yaml"] = testHostK8sOld },
			wantErr: "version 1.33.4 is listed in both host-k8s-1.33.yaml and host-k8s-1.36.yaml",
		},
		{
			name: "default in a component file",
			mutate: func(f map[string]string) {
				f["host-tools.yaml"] = strings.Replace(testHostTool, "- name: tool\n", "- name: tool\n  default: \"1.0.0\"\n", 1)
			},
			wantErr: "host[tool] declares a default",
		},
		{
			name: "empty default in a component file",
			mutate: func(f map[string]string) {
				f["host-tools.yaml"] = strings.Replace(testHostTool, "- name: tool\n", "- name: tool\n  default: \"\"\n", 1)
			},
			wantErr: "host[tool] declares a default",
		},
		{
			name: "null default in a component file",
			mutate: func(f map[string]string) {
				f["cluster.yaml"] = strings.Replace(testCluster, "  release: 'metallb'\n", "  release: 'metallb'\n  default: null\n", 1)
			},
			wantErr: "cluster[metallb] declares a default",
		},
		{
			name:    "second YAML document in a component file",
			mutate:  func(f map[string]string) { f["cluster.yaml"] = testCluster + "---\n" + testCluster },
			wantErr: "cluster.yaml, cause: common.illegal_format: more than one YAML document",
		},
		{
			name:    "second YAML document in defaults.yaml",
			mutate:  func(f map[string]string) { f["defaults.yaml"] = testDefaults + "---\n" + testDefaults },
			wantErr: "defaults.yaml, cause: common.illegal_format: more than one YAML document",
		},
		{
			name:    "unknown key",
			mutate:  func(f map[string]string) { f["cluster.yaml"] = testCluster + "  url: 'x'\n" },
			wantErr: "field url not found",
		},
		{
			name: "chart metadata differs between files",
			mutate: func(f map[string]string) {
				f["cluster-extra.yaml"] = "cluster:\n- name: metallb\n  type: classic\n  repo: 'https://metallb.github.io/metallb'\n" +
					"  chart: 'metallb/metallb'\n  namespace: 'other'\n  release: 'metallb'\n  versions:\n    0.15.2:\n      algorithm: 'sha256'\n      checksum: 'ee'\n"
			},
			wantErr: "cluster[metallb]: type, repo, chart, namespace or release differs between cluster-extra.yaml and cluster.yaml",
		},
		{
			name: "component without a default",
			mutate: func(f map[string]string) {
				f["defaults.yaml"] = "host:\n  kubectl: \"1.36.5\"\ncluster:\n  metallb: \"0.16.1\"\n"
			},
			wantErr: "artifact tool has no default version declared",
		},
		{
			name:    "default for an unknown component",
			mutate:  func(f map[string]string) { f["defaults.yaml"] = testDefaults + "  ghost: \"1.0.0\"\n" },
			wantErr: `cluster default for "ghost", which no catalog file lists`,
		},
		{
			name: "default names a version no file lists",
			mutate: func(f map[string]string) {
				f["defaults.yaml"] = "host:\n  kubectl: \"1.35.0\"\n  tool: \"1.0.0\"\ncluster:\n  metallb: \"0.16.1\"\n"
			},
			wantErr: "1.35.0",
		},
		{
			name: "solo-operator default in defaults file",
			mutate: func(f map[string]string) {
				f["defaults.yaml"] = testDefaults + "  solo-operator: \"0.10.0\"\n"
			},
			wantErr: `cluster default for "solo-operator" must not be set`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := validCatalogFiles()
			tt.mutate(files)
			_, err := loadCatalog(catalogFiles(files))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadCatalog_AllowsLeadingDocumentMarker(t *testing.T) {
	files := validCatalogFiles()
	files["cluster.yaml"] = "---\n" + testCluster
	_, err := loadCatalog(catalogFiles(files))
	require.NoError(t, err)
}

func TestLoadCatalog_InjectsSoloOperatorDefault(t *testing.T) {
	files := validCatalogFiles()
	files["cluster.yaml"] = testCluster + `
- name: solo-operator
  type: oci
  chart: 'oci://ghcr.io/hashgraph/solo-operator/solo-operator-chart'
  namespace: 'solo-operator'
  release: 'solo-operator'
  versions:
    ` + deps.SOLO_OPERATOR_VERSION + `:
      algorithm: 'sha256'
      checksum: 'ff'
`
	catalog, err := loadCatalog(catalogFiles(files))
	require.NoError(t, err)

	chart, err := catalog.GetClusterComponent(SoloOperatorComponentName)
	require.NoError(t, err)
	assert.Equal(t, Version(deps.SOLO_OPERATOR_VERSION), chart.Default)
}
