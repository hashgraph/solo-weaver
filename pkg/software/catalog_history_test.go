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
	"metallb":        {"0.15.2": "4f0fc313cd97819a1c78fff0a389dfe1c9f98bc490f33f521317475df1d7b873"},
	"metrics-server": {"3.13.0": "fb929902e3b7565663cdd1734f31d63735c932be1541a7228b5aeef6d2348a1f"},
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
		"metallb":        {"0.16.1", "fb06bb584fcb7856f15733b2a6a2aff5b61b5c350687e341c163ae24a5938adc"},
		"metrics-server": {"3.14.0", "c2ca1185c01e6e7f53dd1b7d131f0c9b3fa50e003ed068b784563a1b5a3422a1"},
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
