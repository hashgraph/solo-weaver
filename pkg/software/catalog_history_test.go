// SPDX-License-Identifier: Apache-2.0

package software

import (
	"fmt"
	"path"
	"sort"
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

// These host entries are pinned field by field. The first version of each was
// installed by earlier releases, and VerifyExecutables still checks those hosts
// against it; the second is the version new installs get. Every checksum was
// verified against the upstream release when the entry was added.
var pinnedHostEntries = map[string]map[string][]string{
	"helm": {
		"3.18.6": {
			"archive helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz https://get.helm.sh/helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/amd64 sha256:3f43c0aa57243852dd542493a0f54f1396c0bc8ec7296bbb2c01e802010819ce",
			"archive helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz https://get.helm.sh/helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/arm64 sha256:5b8e00b6709caab466cbbb0bc29ee09059b8dc9417991dd04b497530e49b1737",
			"binary {{.OS}}-{{.ARCH}}/helm helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/amd64 sha256:c153fd9c1173f39aefe8e9aa9f00fd3daf6b40c8ea01e94a0d2f2c1787fc60e0",
			"binary {{.OS}}-{{.ARCH}}/helm helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/arm64 sha256:c7550b38b3b7c9df31f7052432b5eb45e0cfec7238310de0133789598c2449e4",
		},
		"3.22.1": {
			"archive helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz https://get.helm.sh/helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/amd64 sha256:896cf63684a42a723d61c980369d6e588330927deb13676ca0d85006dd6da0ea",
			"archive helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz https://get.helm.sh/helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/arm64 sha256:4c29e286b279d170fa048ddfc3e4e83dcd069ca9c52102766ba672d7149e694d",
			"binary {{.OS}}-{{.ARCH}}/helm helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/amd64 sha256:46aeab5e043f62d01f7c168823b392cabc1cea3807b67906296290483791be43",
			"binary {{.OS}}-{{.ARCH}}/helm helm-v{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz linux/arm64 sha256:2bd1b649ef89175bc0ae1e0fa7de7e0583a5b0401de9896b644b8acbcae1ba64",
		},
	},
	"k9s": {
		"0.50.9": {
			"archive k9s_{{.OS}}_{{.ARCH}}.tar.gz https://github.com/derailed/k9s/releases/download/v{{.VERSION}}/k9s_{{.OS}}_{{.ARCH}}.tar.gz linux/amd64 sha256:5e625efa26c3e14256cf29d242179b32129183b549937ec62d0ad9be1bfe2ca4",
			"archive k9s_{{.OS}}_{{.ARCH}}.tar.gz https://github.com/derailed/k9s/releases/download/v{{.VERSION}}/k9s_{{.OS}}_{{.ARCH}}.tar.gz linux/arm64 sha256:147e696b608da9a300b9db6b35fd6dfc0713ce8c64865e550c90452696bb779a",
			"binary k9s k9s_{{.OS}}_{{.ARCH}}.tar.gz linux/amd64 sha256:492f8906febc010a12b03cb69a9dc47c9bd9d4b20abcf6d30d75ae304813547d",
			"binary k9s k9s_{{.OS}}_{{.ARCH}}.tar.gz linux/arm64 sha256:26bcae69dd653838db00a0e777aa52e87877c325a06ad6f625857604c02e9efc",
		},
		"0.51.0": {
			"archive k9s_Linux_{{.ARCH}}.tar.gz https://github.com/derailed/k9s/releases/download/v{{.VERSION}}/k9s_Linux_{{.ARCH}}.tar.gz linux/amd64 sha256:c3752ad51a5a4015a113819c4eeb6e55a4d0e4b8e652494797532f6fc8161dd7",
			"archive k9s_Linux_{{.ARCH}}.tar.gz https://github.com/derailed/k9s/releases/download/v{{.VERSION}}/k9s_Linux_{{.ARCH}}.tar.gz linux/arm64 sha256:3ee05c82e5f9198928a4e86133608ba6a2c10a2244d6a7789e820f78319d640c",
			"binary k9s k9s_Linux_{{.ARCH}}.tar.gz linux/amd64 sha256:da2a7b4844204e5f31da0e57536e85a848f4d2260cd58737402bbb04ce4c99a2",
			"binary k9s k9s_Linux_{{.ARCH}}.tar.gz linux/arm64 sha256:92ed2bc895004d08d080d08a9cff9e85d38603e6a34a8df15761481de7533801",
		},
	},
	"teleport": {
		"18.6.4": {
			"archive teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz https://cdn.teleport.dev/teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:41232c53ba798946fba27b69496c6f0489bc38cc191b622e0728f7078011343f",
			"archive teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz https://cdn.teleport.dev/teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:af6aa427990c9a832d16eab33a2c2f41f5f8ed138e43618be84992e9e5add03a",
			"binary teleport-ent/tctl teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:855f0000d933feac03ac224da16f9062a6b7b3745e826f3757af5622832d9023",
			"binary teleport-ent/tctl teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:b993c93a32e4b679d957bc047c65dee04a9d9cffe150d77b567d1e63919988d8",
			"binary teleport-ent/teleport teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:ec864b1f1a3a826ebb9ddecea76904df721ce60e21fdfb58fa6d4631aad1c3df",
			"binary teleport-ent/teleport teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:80dc7fc128794682405dd3bff02cd99e12015b6c22b05ec941ef3068db1bb915",
			"binary teleport-ent/teleport-update teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:f692e1d70840807d53d417fc235eed8ba7a47fc7f607bfa1d1b2b94bfecc7559",
			"binary teleport-ent/teleport-update teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:e93ab32f25460ccbd581112bba753a2d0c7c0d71ecb41baee238441083db2e7c",
			"binary teleport-ent/tsh teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:ec368495e2084e2749303a67f336e6bddc504d6b64576051b8103ff25538cb76",
			"binary teleport-ent/tsh teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:855c5efaa483ac15022b4f809a5e8d2a4265bdfaa236d60feb271183a34c17fd",
			"config teleport-ent/examples/systemd/production/node/teleport.service teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz sha256:d3a4773d2402b72a257ab187b8a2771d45747876320679fe8eaf267ed1a40c41",
		},
		"18.11.3": {
			"archive teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz https://cdn.teleport.dev/teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:0be29e66c71e00162d40bc41d62f62c595d76654315c4dadadd6414a1d385f14",
			"archive teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz https://cdn.teleport.dev/teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:8631d9003bd0228ff80477e1dc8cd30eeff3144ae615edffe1bfd3eaf3d4c4c6",
			"binary teleport-ent/tctl teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:a5c50967f40493f158c715b5298baa8f950c46064934b24f2fa1685e856ec386",
			"binary teleport-ent/tctl teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:bc2b9fd05b97917ee112cfb5776f97fd62d48310e84e3be0d221d8119274446b",
			"binary teleport-ent/teleport teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:0c7d4f41fceb30e7968d68aa2f545bc9888a6b832d062f7b0e3a2b6f92014940",
			"binary teleport-ent/teleport teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:f3d27cfd213353725440a9ccee5f5278a249f249f5f56db50307d7ad976dff4d",
			"binary teleport-ent/teleport-update teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:dc0f3a5d55d6e9714a16a9fbb8d4075d0b94253265364ca7e42d87d6c3a5b117",
			"binary teleport-ent/teleport-update teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:7bf8d1af2885de24812dd6408bb77f2c8dce9fc88e0aa3f80e0b2fad42c8d62d",
			"binary teleport-ent/tsh teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/amd64 sha256:eba0132268ee3140b14fc3b2691b8e2200bc1aa25058a104a122e28ed8e7f9bb",
			"binary teleport-ent/tsh teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz linux/arm64 sha256:df329b6a344c323085ae3fb28372d9bcd20a80fed345595a7d81821605e7c2f3",
			"config teleport-ent/examples/systemd/production/node/teleport.service teleport-ent-v{{.VERSION}}-{{.OS}}-{{.ARCH}}-bin.tar.gz sha256:d3a4773d2402b72a257ab187b8a2771d45747876320679fe8eaf267ed1a40c41",
		},
	},
}

func TestCatalog_PinnedHostEntriesAreUnchanged(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for name, versions := range pinnedHostEntries {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		for v, want := range versions {
			info, ok := artifact.Versions[Version(v)]
			require.True(t, ok, "%s %s must stay in the catalog", name, v)
			assert.Equal(t, want, hostEntryLines(info), "%s %s", name, v)
		}
	}
}

// hostEntryLines flattens a catalog version into one sorted line per artifact
// and platform, so a test can compare every field at once.
func hostEntryLines(info VersionDetails) []string {
	platforms := func(pc PlatformChecksum) []string {
		var out []string
		for _, arch := range []string{"amd64", "arm64"} {
			c := pc["linux"][arch]
			out = append(out, fmt.Sprintf("linux/%s %s:%s", arch, c.Algorithm, c.Value))
		}
		return out
	}

	var lines []string
	for _, a := range info.Archives {
		for _, p := range platforms(a.PlatformChecksum) {
			lines = append(lines, fmt.Sprintf("archive %s %s %s", a.Name, a.URL, p))
		}
	}
	for _, b := range info.Binaries {
		for _, p := range platforms(b.PlatformChecksum) {
			lines = append(lines, fmt.Sprintf("binary %s %s %s", b.Name, b.Archive, p))
		}
	}
	for _, c := range info.Configs {
		lines = append(lines, fmt.Sprintf("config %s %s %s:%s", c.Name, c.Archive, c.Algorithm, c.Value))
	}
	sort.Strings(lines)
	return lines
}

// IsInstalled looks for the default version's binaries. If another version of
// these artifacts listed a different set, hosts running it would look absent and
// the install step would replace them with the default.
func TestCatalog_EveryVersionInstallsTheSameBinaries(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for _, name := range []string{"helm", "k9s", "teleport"} {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		defaultVersion, err := artifact.GetDefaultVersion()
		require.NoError(t, err)
		want := binaryBasenames(artifact.Versions[Version(defaultVersion)])
		for v, info := range artifact.Versions {
			assert.Equal(t, want, binaryBasenames(info), "%s %s", name, v)
		}
	}
}

func binaryBasenames(info VersionDetails) []string {
	var names []string
	for _, b := range info.GetBinaries() {
		names = append(names, path.Base(b.Name))
	}
	return names
}

// Clusters built by earlier releases still run these charts. Their checksums
// must not change, and metrics-server 3.13.0 is also what a cluster older than
// Kubernetes 1.34 gets when its release is missing.
var chartVersionsOnExistingClusters = map[string]map[string]string{
	"metallb":                  {"0.15.2": "4f0fc313cd97819a1c78fff0a389dfe1c9f98bc490f33f521317475df1d7b873"},
	"metrics-server":           {"3.13.0": "fb929902e3b7565663cdd1734f31d63735c932be1541a7228b5aeef6d2348a1f"},
	"alloy":                    {"1.4.0": "b1adbb6344301b8353c9d5d39bb5de196e3735441ca1742913d60922a96bffcb"},
	"prometheus-operator-crds": {"24.0.1": "e940c5ce899ec16aa6d868ecf49e2c21193728bef3a0a453b29e05e2d1601288"},
	"teleport-cluster-agent":   {"18.6.4": "f657f1f216ebfe6bd074d5c3509b6617205ae956d3e73b895e20d71b26002201"},
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
