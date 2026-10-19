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
