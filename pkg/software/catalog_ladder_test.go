// SPDX-License-Identifier: Apache-2.0

package software

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The upgrade ladder: the latest patch of each supported Kubernetes minor.
var kubernetesRungs = map[string]string{
	"1.33": "1.33.13",
	"1.34": "1.34.13",
	"1.35": "1.35.10",
	"1.36": "1.36.5",
}

func TestCatalog_KubernetesRungs(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	kubelet, err := catalog.GetHostArtifact("kubelet")
	require.NoError(t, err)
	latest := map[string]string{}
	for _, v := range sortedVersions(kubelet) { // ascending, so the last of each minor wins
		minor, err := minorOf(v)
		require.NoError(t, err)
		latest[minor] = v
	}
	assert.Equal(t, kubernetesRungs, latest)

	// kubeadm and kubectl ship every kubelet version, and cri-o has a version
	// of every kubelet minor.
	for _, name := range []string{"kubeadm", "kubectl"} {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		assert.Equal(t, sortedVersions(kubelet), sortedVersions(artifact), name)
	}
	crio, err := catalog.GetHostArtifact("cri-o")
	require.NoError(t, err)
	crioMinors := map[string]bool{}
	for _, v := range sortedVersions(crio) {
		minor, err := minorOf(v)
		require.NoError(t, err)
		crioMinors[minor] = true
	}
	for minor := range kubernetesRungs {
		assert.True(t, crioMinors[minor], "no cri-o %s version", minor)
	}
}

func TestCatalog_CiliumRungs(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	chart, err := catalog.GetClusterComponent(CiliumComponentName)
	require.NoError(t, err)
	assert.Equal(t, Version("1.20.2"), chart.Default)

	want := map[Version]KubernetesRange{
		"1.18.16": {Min: "1.30", Max: "1.33"},
		"1.19.10": {Min: "1.31", Max: "1.34"},
		"1.20.2":  {Min: "1.33", Max: "1.36"},
	}
	got := map[Version]KubernetesRange{}
	for v, details := range chart.Versions {
		// The install step pulls the chart with a sha256 check.
		assert.Equal(t, "sha256", details.Algorithm, v)
		require.NotNil(t, details.Kubernetes, "cilium %s has no kubernetes range", v)
		got[v] = *details.Kubernetes
	}
	assert.Equal(t, want, got)
}

func TestCatalog_KubernetesChartMinimums(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	got := map[string]map[string]Version{}
	for _, k := range catalog.Kubernetes {
		got[k.Minor] = k.Charts
	}
	assert.Equal(t, map[string]map[string]Version{
		"1.33": {},
		"1.34": {},
		"1.35": {},
		"1.36": {"metallb": "0.16.1"},
	}, got)
}
