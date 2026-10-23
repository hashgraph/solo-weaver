// SPDX-License-Identifier: Apache-2.0

package software

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hosts provisioned by earlier releases still run these versions. The machine
// checker and VerifyExecutables only recognise a binary whose version the
// catalog still lists, so dropping one when a default moves breaks those hosts.
var versionsOnExistingHosts = map[string][]string{
	"cri-o":    {"1.33.4"},
	"kubelet":  {"1.33.4"},
	"kubeadm":  {"1.33.4"},
	"kubectl":  {"1.33.4"},
	"cilium":   {"0.18.7"},
	"helm":     {"3.18.6"},
	"k9s":      {"0.50.9"},
	"teleport": {"18.6.4"},
}

func TestCatalog_KeepsVersionsInstalledByEarlierReleases(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for name, versions := range versionsOnExistingHosts {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		for _, v := range versions {
			_, ok := artifact.Versions[Version(v)]
			assert.True(t, ok, "%s %s must stay in the catalog while existing hosts may run it", name, v)
		}
	}
}

// Clusters built by earlier releases still run these charts. Their checksums
// must not change, and metrics-server 3.13.0 is also what a cluster older than
// Kubernetes 1.34 gets when its release is missing.
var chartVersionsOnExistingClusters = map[string]map[string]string{
	"metallb":                  {"0.15.2": "4f0fc313cd97819a1c78fff0a389dfe1c9f98bc490f33f521317475df1d7b873"},
	"metrics-server":           {"3.13.0": "fb929902e3b7565663cdd1734f31d63735c932be1541a7228b5aeef6d2348a1f"},
	"alloy":                    {"1.4.0": "b1adbb6344301b8353c9d5d39bb5de196e3735441ca1742913d60922a96bffcb"},
	"prometheus-operator-crds": {"24.0.1": "e940c5ce899ec16aa6d868ecf49e2c21193728bef3a0a453b29e05e2d1601288"},
}

func TestCatalog_KeepsChartVersionsDeployedByEarlierReleases(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for name, versions := range chartVersionsOnExistingClusters {
		chart, err := catalog.GetClusterComponent(name)
		require.NoError(t, err)
		for v, checksum := range versions {
			sum, ok := chart.Versions[Version(v)]
			require.True(t, ok, "%s %s must stay in the catalog while existing clusters may run it", name, v)
			assert.Equal(t, "sha256", sum.Algorithm)
			assert.Equal(t, checksum, sum.Value, "%s %s checksum", name, v)
		}
	}
}

func TestCatalog_ClusterChartDefaultsForKubernetes136(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for name, want := range map[string]struct{ version, checksum string }{
		"metallb":                  {"0.16.1", "fb06bb584fcb7856f15733b2a6a2aff5b61b5c350687e341c163ae24a5938adc"},
		"metrics-server":           {"3.14.0", "c2ca1185c01e6e7f53dd1b7d131f0c9b3fa50e003ed068b784563a1b5a3422a1"},
		"alloy":                    {"1.13.1", "6c2b7df23e39438bb11f1e1fcf19eb9eb7c8bfeab7d49b7c975b6212abd0f99a"},
		"prometheus-operator-crds": {"32.0.1", "6a5f0e0d7e53f0c9336d6ff56fbf4ff506a1a0d8251a26567b11cbbdef2c01b2"},
	} {
		chart, err := catalog.GetClusterComponent(name)
		require.NoError(t, err)
		version, err := chart.GetDefaultVersion()
		require.NoError(t, err)
		assert.Equal(t, want.version, version, name)
		sum, ok := chart.Versions[Version(want.version)]
		require.True(t, ok, name)
		assert.Equal(t, want.checksum, sum.Value, name)
	}
}

// Verification fails closed on a platform without a checksum, so every listed
// binary needs one for each platform we ship.
func TestCatalog_HostBinariesHaveLinuxChecksums(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for _, artifact := range catalog.Host {
		for version, info := range artifact.Versions {
			for _, binary := range info.GetBinaries() {
				for _, arch := range []string{"amd64", "arm64"} {
					_, ok := binary.PlatformChecksum["linux"][arch]
					assert.True(t, ok, "%s %s %s has no linux/%s checksum", artifact.Name, version, binary.Name, arch)
				}
			}
		}
	}
}
