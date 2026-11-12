// SPDX-License-Identifier: Apache-2.0

package software

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKubelet = `
host:
- name: kubelet
  versions:
    1.35.10:
      binaries:
        - name: 'kubelet'
          url: 'https://example.com/kubelet'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: 'k1'
    1.36.5:
      binaries:
        - name: 'kubelet'
          url: 'https://example.com/kubelet'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: 'k2'
`

const testCompatCluster = `
cluster:
- name: metallb
  type: classic
  repo: 'https://metallb.github.io/metallb'
  chart: 'metallb/metallb'
  namespace: 'metallb-system'
  release: 'metallb'
  versions:
    0.15.2:
      algorithm: 'sha256'
      checksum: 'm1'
    0.16.1:
      algorithm: 'sha256'
      checksum: 'm2'
- name: cilium
  type: classic
  repo: 'https://helm.cilium.io'
  chart: 'cilium/cilium'
  namespace: 'kube-system'
  release: 'cilium'
  versions:
    1.20.2:
      algorithm: 'sha256'
      checksum: 'c1'
      kubernetes: {min: '1.33', max: '1.36'}
`

const testCompatDefaults = `
host:
  kubelet: "1.36.5"
cluster:
  metallb: "0.16.1"
  cilium: "1.20.2"
`

func compatCatalogFiles() map[string]string {
	return map[string]string{
		"defaults.yaml":     testCompatDefaults,
		"cluster.yaml":      testCompatCluster,
		"host-kubelet.yaml": testKubelet,
		"k8s-1.35.yaml":     "kubernetes:\n- minor: '1.35'\n  charts: {}\n",
		"k8s-1.36.yaml":     "kubernetes:\n- minor: '1.36'\n  charts:\n    metallb: '0.16.1'\n",
	}
}

func TestLoadCatalog_KubernetesCompatibility(t *testing.T) {
	catalog, err := loadCatalog(catalogFiles(compatCatalogFiles()))
	require.NoError(t, err)

	assert.Equal(t, []KubernetesMinor{
		{Minor: "1.35", Charts: map[string]Version{}},
		{Minor: "1.36", Charts: map[string]Version{"metallb": "0.16.1"}},
	}, catalog.Kubernetes)

	cilium, err := catalog.GetClusterComponent(CiliumComponentName)
	require.NoError(t, err)
	assert.Equal(t, &KubernetesRange{Min: "1.33", Max: "1.36"}, cilium.Versions["1.20.2"].Kubernetes)

	metallb, err := catalog.GetClusterComponent("metallb")
	require.NoError(t, err)
	assert.Nil(t, metallb.Versions["0.16.1"].Kubernetes)
}

func TestLoadCatalog_KubernetesCompatibilityErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(files map[string]string)
		wantErr string
	}{
		{
			name: "reversed range",
			mutate: func(f map[string]string) {
				replaceIn(f, "cluster.yaml", "{min: '1.33', max: '1.36'}", "{min: '1.36', max: '1.33'}")
			},
			wantErr: "kubernetes range 1.36 to 1.33 is reversed",
		},
		{
			name:    "range minor with a patch",
			mutate:  func(f map[string]string) { replaceIn(f, "cluster.yaml", "max: '1.36'", "max: '1.36.5'") },
			wantErr: `"1.36.5" is not a Kubernetes minor`,
		},
		{
			name:    "range minor not a number",
			mutate:  func(f map[string]string) { replaceIn(f, "cluster.yaml", "min: '1.33'", "min: 'one'") },
			wantErr: `"one" is not a Kubernetes minor`,
		},
		{
			name:    "range missing its max",
			mutate:  func(f map[string]string) { replaceIn(f, "cluster.yaml", ", max: '1.36'", "") },
			wantErr: `"" is not a Kubernetes minor`,
		},
		{
			name:    "minor listed in two files",
			mutate:  func(f map[string]string) { f["k8s-1.36-copy.yaml"] = f["k8s-1.36.yaml"] },
			wantErr: "kubernetes[1.36] is listed in both k8s-1.36-copy.yaml and k8s-1.36.yaml",
		},
		{
			name:    "malformed minor",
			mutate:  func(f map[string]string) { replaceIn(f, "k8s-1.35.yaml", "'1.35'", "'v1.35'") },
			wantErr: `"v1.35" is not a Kubernetes minor`,
		},
		{
			name:    "minimum names a chart the catalog does not have",
			mutate:  func(f map[string]string) { replaceIn(f, "k8s-1.36.yaml", "metallb:", "ghost:") },
			wantErr: `kubernetes[1.36]: chart "ghost" is not in the catalog`,
		},
		{
			name:    "minimum is not a listed version",
			mutate:  func(f map[string]string) { replaceIn(f, "k8s-1.36.yaml", "'0.16.1'", "'0.16.0'") },
			wantErr: "kubernetes[1.36]: metallb 0.16.0 is not a listed version",
		},
		{
			name:    "kubelet minor without an entry",
			mutate:  func(f map[string]string) { delete(f, "k8s-1.35.yaml") },
			wantErr: "kubelet has 1.35 versions but no kubernetes entry lists minor 1.35",
		},
		{
			name:    "entry for a minor with no kubelet",
			mutate:  func(f map[string]string) { f["k8s-1.37.yaml"] = "kubernetes:\n- minor: '1.37'\n  charts: {}\n" },
			wantErr: "kubernetes[1.37]: no kubelet version of this minor",
		},
		{
			name:    "chart default below the default minor's minimum",
			mutate:  func(f map[string]string) { replaceIn(f, "defaults.yaml", `metallb: "0.16.1"`, `metallb: "0.15.2"`) },
			wantErr: "kubernetes[1.36] is the default kubelet's minor and needs metallb 0.16.1, but the default is 0.15.2",
		},
		{
			name: "unknown key in an entry",
			mutate: func(f map[string]string) {
				replaceIn(f, "k8s-1.35.yaml", "  charts: {}\n", "  charts: {}\n  cilium: '1.19'\n")
			},
			wantErr: "field cilium not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := compatCatalogFiles()
			tt.mutate(files)
			_, err := loadCatalog(catalogFiles(files))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// An older minor's minimum need not be met by the defaults: only the default
// kubelet's minor is checked.
func TestLoadCatalog_OnlyTheDefaultMinorBindsTheDefaults(t *testing.T) {
	files := compatCatalogFiles()
	files["k8s-1.35.yaml"] = "kubernetes:\n- minor: '1.35'\n  charts:\n    metallb: '0.16.1'\n"
	files["k8s-1.36.yaml"] = "kubernetes:\n- minor: '1.36'\n  charts: {}\n"
	replaceIn(files, "defaults.yaml", `metallb: "0.16.1"`, `metallb: "0.15.2"`)

	_, err := loadCatalog(catalogFiles(files))
	require.NoError(t, err)
}

func TestParseMinor(t *testing.T) {
	got, err := parseMinor("1.34")
	require.NoError(t, err)
	assert.Equal(t, [2]int{1, 34}, got)

	for _, bad := range []string{"", "1", "1.", ".34", "1.34.0", "a.b", "-1.34", "1.-34"} {
		_, err := parseMinor(bad)
		assert.Error(t, err, bad)
	}
}

func TestMinorOf(t *testing.T) {
	got, err := minorOf("1.34.13")
	require.NoError(t, err)
	assert.Equal(t, "1.34", got)

	for _, bad := range []string{"", "1", "x.y.z"} {
		_, err := minorOf(bad)
		assert.Error(t, err, bad)
	}
}

func replaceIn(files map[string]string, name, old, new string) {
	if !strings.Contains(files[name], old) {
		panic(name + " has no " + old)
	}
	files[name] = strings.Replace(files[name], old, new, 1)
}
