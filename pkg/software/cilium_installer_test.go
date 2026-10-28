// SPDX-License-Identifier: Apache-2.0

package software

import (
	"regexp"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_ciliumConfigTemplate_HostLegacyRoutingAndLoadBalancer pins the
// load-bearing Cilium ConfigMap values the BN traffic shaper depends on
// (issue #741): host legacy routing must be enabled and the load-balancer mode
// must stay DSR. These render statically (no template data), so a direct render
// is enough to catch an accidental flip.
func Test_ciliumConfigTemplate_HostLegacyRoutingAndLoadBalancer(t *testing.T) {
	rendered, err := templates.Render(ciliumTemplateFile, struct {
		SandboxDir string
		MachineIP  string
	}{
		SandboxDir: "/opt/weaver",
		MachineIP:  "10.0.0.1",
	})
	require.NoError(t, err)

	// enable-host-legacy-routing: "true" — rendered from bpf.hostLegacyRouting.
	require.Contains(t, rendered, "hostLegacyRouting: true")
	require.NotContains(t, rendered, "hostLegacyRouting: false")

	// loadBalancer.mode must remain DSR; BandwidthManager must not be enabled in
	// the template (it stays at the Cilium default of Disabled).
	require.Contains(t, rendered, "mode: dsr")
	require.NotContains(t, rendered, "bandwidthManager")
}

func TestCiliumCatalog_DefaultsToCLI0201AndKeeps0187(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)
	artifact, err := catalog.GetHostArtifact(CiliumBinaryName)
	require.NoError(t, err)

	def, err := artifact.GetDefaultVersion()
	require.NoError(t, err)
	assert.Equal(t, "0.20.1", def)

	sha256Hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, version := range []Version{"0.20.1", "0.18.7"} {
		info, ok := artifact.Versions[version]
		require.True(t, ok, "cilium %s must be in the catalog", version)
		require.Len(t, info.Archives, 1, "cilium %s archives", version)
		require.Len(t, info.Binaries, 1, "cilium %s binaries", version)

		for _, arch := range []string{"amd64", "arm64"} {
			archive := info.Archives[0].PlatformChecksum["linux"][arch]
			binary := info.Binaries[0].PlatformChecksum["linux"][arch]
			for kind, sum := range map[string]Checksum{"archive": archive, "binary": binary} {
				assert.Equal(t, "sha256", sum.Algorithm, "cilium %s linux/%s %s algorithm", version, arch, kind)
				assert.Regexp(t, sha256Hex, sum.Value, "cilium %s linux/%s %s checksum", version, arch, kind)
			}
			assert.NotEqual(t, archive.Value, binary.Value, "cilium %s linux/%s archive and binary checksums", version, arch)
		}
	}
}

// An existing host keeps CLI 0.18.7 after the default moves to 0.20.1. The real
// catalog entry is used with its checksums swapped for known test bytes, so the
// check exercises the version layout the provisioner ships.
func TestCiliumCatalog_InstalledCLIStillVerifiesAfterDefaultChange(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)
	shipped, err := catalog.GetHostArtifact(CiliumBinaryName)
	require.NoError(t, err)

	artifact := &ArtifactMetadata{Name: shipped.Name, Default: shipped.Default, Versions: map[Version]VersionDetails{}}
	sums := map[Version]string{}
	for version, info := range shipped.Versions {
		_, sum := writeTempBinary(t, "cilium-cli-"+string(version))
		sums[version] = sum
		binaries := make([]BinaryDetail, len(info.Binaries))
		for i, b := range info.Binaries {
			b.PlatformChecksum = linuxAmd64Checksum("sha256", sum)
			binaries[i] = b
		}
		artifact.Versions[version] = VersionDetails{Archives: info.Archives, Binaries: binaries}
	}
	artifact = artifact.withPlatform("linux", "amd64")
	require.Equal(t, Version("0.20.1"), artifact.Default)

	installedPath, _ := writeTempBinary(t, "cilium-cli-0.18.7")
	resolve := func(string) string { return installedPath }

	require.NoError(t, verifyInstalledExecutables(artifact, "0.18.7", resolve), "recorded 0.18.7 must verify against its own checksum")
	require.NoError(t, verifyInstalledExecutables(artifact, "", resolve), "an unrecorded 0.18.7 must be identified, not checked against the default")

	matches, err := identifyArtifactVersions(artifact, resolve)
	require.NoError(t, err)
	assert.Equal(t, []string{"0.18.7"}, matches)

	err = verifyInstalledExecutables(artifact, "0.20.1", resolve)
	require.Error(t, err, "a 0.18.7 binary recorded as 0.20.1 is an integrity failure")
	assert.True(t, errorx.IsOfType(err, ChecksumError), "expected ChecksumError, got %v", err)
}
