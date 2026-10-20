// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/templates"
)

var kubernetesDefaults = map[string]string{
	"cri-o":   "1.36.7",
	"kubelet": "1.36.5",
	"kubeadm": "1.36.5",
	"kubectl": "1.36.5",
}

func TestCatalog_KubernetesDefaults(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for name, want := range kubernetesDefaults {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		got, err := artifact.GetDefaultVersion()
		require.NoError(t, err)
		assert.Equal(t, want, got, name)
	}
}

type pinnedChecksum struct{ artifact, file, arch string }

// The exact bytes existing hosts run. Changing any of these makes their
// binaries unverifiable.
var kubernetes1334Checksums = map[pinnedChecksum]string{
	{"cri-o", "cri-o.{{.ARCH}}.v{{.VERSION}}.tar.gz", "amd64"}: "8f6d14828659b85da7c83bad798d50c2f7e0311742615fb7ed305f77bab54e50",
	{"cri-o", "cri-o.{{.ARCH}}.v{{.VERSION}}.tar.gz", "arm64"}: "6a04cb1ab2020508927d7237ff1174bb330211a1076683417b30642a9c8e4996",
	{"cri-o", "cri-o/bin/crio", "amd64"}:                       "f731643789ef88e5db1bafac1cccaf82b3e5ae015c0fe18a3dac6341a54efc19",
	{"cri-o", "cri-o/bin/crio", "arm64"}:                       "a1b8136305ba9f7963e875e170c7fe98955d3e2da2e5ce47cf66614b0aa658ba",
	{"cri-o", "cri-o/bin/crun", "amd64"}:                       "a8bd8ac68b8162ba23c10f8868a5374cfb13a8288a66f559824918cec058a1c9",
	{"cri-o", "cri-o/bin/crun", "arm64"}:                       "e18f7fae42b1670e9fceb23f50296f980c9e2985a6324bc85b1c30f28c44a407",
	{"cri-o", "cri-o/bin/conmonrs", "amd64"}:                   "84796ecf0a40ef40f212b38f4fcae1cf0935fce0b10b0fb43ca885148fcef7b3",
	{"cri-o", "cri-o/bin/conmonrs", "arm64"}:                   "e5774b918335fa706ef58faec55d143e598c66b9109765b4e56c7583345f84a9",
	{"cri-o", "cri-o/bin/conmon", "amd64"}:                     "aefa6f2e5f1eeddd65be1b2991a5852f7c2ab192a298eee19800bd79d788e4be",
	{"cri-o", "cri-o/bin/conmon", "arm64"}:                     "d1257e78795b9cb57b5468a853029ad6ec573b89b9416fee22f7e15aa8976761",
	{"cri-o", "cri-o/bin/runc", "amd64"}:                       "028986516ab5646370edce981df2d8e8a8d12188deaf837142a02097000ae2f2",
	{"cri-o", "cri-o/bin/runc", "arm64"}:                       "85c5e4e4f72e442c8c17bac07527cd4f961ee48e4f2b71797f7533c94f4a52b9",
	{"cri-o", "cri-o/bin/pinns", "amd64"}:                      "e1897c3d35a744eabff72be1368b9cf4073f54d1edc1ef59954b5fe4237e8a1f",
	{"cri-o", "cri-o/bin/pinns", "arm64"}:                      "64e9f973f43adec511422050cc3684a042b3f600900df58b8e3ef7cdc022ffeb",
	{"cri-o", "cri-o/bin/crictl", "amd64"}:                     "9b49f0acab34e9f0eff218b493d92688f53200582ebc67d8cc01e9770da5c955",
	{"cri-o", "cri-o/bin/crictl", "arm64"}:                     "adcd45619f1f57eb38f8596bf65def237c53fb00b9f42b963f8a1556bb0918bb",
	{"kubelet", "kubelet", "amd64"}:                            "8b56125984b88bd261d8ebde54fbcf294ca0c49f907b41aa03edcafe7c17699a738164cfa5bcba08ff0475c17343ab18c085256169e46acde03d8ac98fe08de7",
	{"kubelet", "kubelet", "arm64"}:                            "629083fb429f606bb30c84ab654a1ca3af8cde3e57700cdc40992ebdd28679cadc55119a77da2275d4082fd3813b2594892a4c65968c2caec957aaa628646a30",
	{"kubeadm", "kubeadm", "amd64"}:                            "10ba79eaf4a42f3d8761e883858ceb9ebb120cace52380fa4a7083bd076139dd26a18ec379143d32476a11c70de98d89801afb30e585bd1169eae75e880c7921",
	{"kubeadm", "kubeadm", "arm64"}:                            "7a6f897ee8b4961e6787bdbffa5f03c5635981e3d5f9c752651b1455b75888ee5bf9528794d828e7e6aeb1161554871f20716024cacb88d1038db706f440a10b",
	{"kubectl", "kubectl", "amd64"}:                            "b2b9d3ab3731f0b2a740d04f442e2c85c154ecf6fa65146d7a4d08297e8f08bc075eeda9bb0aad3de0a2ea432eaa40d8f20177208ef5eb84b1dac192129a21c5",
	{"kubectl", "kubectl", "arm64"}:                            "f76edb18df3e807ae664c2b3833e210f29ba2fe5ff53624f0787b47989dfc00d71da43c208a06b4f548f857893f19003e34523cb09bf588f81b3f06610b13337",
}

func TestCatalog_Kubernetes1334ChecksumsUnchanged(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	got := map[pinnedChecksum]string{}
	for name := range kubernetesDefaults {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		info, ok := artifact.Versions["1.33.4"]
		require.True(t, ok, "%s 1.33.4 must stay in the catalog", name)
		for _, a := range info.GetArchives() {
			for arch, c := range a.PlatformChecksum["linux"] {
				got[pinnedChecksum{name, a.Name, arch}] = c.Value
			}
		}
		for _, b := range info.GetBinaries() {
			for arch, c := range b.PlatformChecksum["linux"] {
				got[pinnedChecksum{name, b.Name, arch}] = c.Value
			}
		}
	}
	assert.Equal(t, kubernetes1334Checksums, got)
}

// Upstream publishes sha512 for Kubernetes binaries and sha256 for the cri-o
// archive; a wrong algorithm or a truncated digest would fail every install.
func TestCatalog_KubernetesChecksumFormat(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	wantLen := map[string]int{"sha256": 64, "sha512": 128}
	for name := range kubernetesDefaults {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		wantAlg := "sha512"
		if name == CrioArtifactName {
			wantAlg = "sha256"
		}
		for version, info := range artifact.Versions {
			var sums []PlatformChecksum
			for _, a := range info.GetArchives() {
				sums = append(sums, a.PlatformChecksum)
			}
			for _, b := range info.GetBinaries() {
				sums = append(sums, b.PlatformChecksum)
			}
			for _, pc := range sums {
				for _, arch := range []string{"amd64", "arm64"} {
					c, ok := pc["linux"][arch]
					require.True(t, ok, "%s %s has no linux/%s checksum", name, version, arch)
					assert.Equal(t, wantAlg, c.Algorithm, "%s %s %s", name, version, arch)
					assert.Len(t, c.Value, wantLen[c.Algorithm], "%s %s %s", name, version, arch)
					assert.Equal(t, strings.ToLower(c.Value), c.Value)
				}
			}
		}
	}
}

// The machine checker identifies an unrecorded binary by checksum, so no two
// versions may share a full binary set on any platform.
func TestCatalog_KubernetesVersionsAreDistinguishable(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for name := range kubernetesDefaults {
		artifact, err := catalog.GetHostArtifact(name)
		require.NoError(t, err)
		for _, arch := range []string{"amd64", "arm64"} {
			seen := map[string]Version{}
			for version, info := range artifact.Versions {
				var sums []string
				for _, b := range info.GetBinaries() {
					sums = append(sums, path.Base(b.Name)+"="+b.PlatformChecksum["linux"][arch].Value)
				}
				sort.Strings(sums)
				key := strings.Join(sums, ",")
				other, dup := seen[key]
				assert.False(t, dup, "%s %s and %s have identical %s binaries", name, version, other, arch)
				seen[key] = version
			}
		}
	}
}

// Install copies, and VerifyExecutables hashes, exactly the binaries in
// crioInstalledBinaryPaths. Every catalog version must list that same set, or
// a binary would be installed unverified or verified at a path never written.
func TestCatalog_CrioBinariesMatchInstallLayout(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)
	artifact, err := catalog.GetHostArtifact(CrioArtifactName)
	require.NoError(t, err)

	var layout []string
	for name := range crioInstalledBinaryPaths("/sandbox") {
		layout = append(layout, name)
	}
	sort.Strings(layout)

	for version, info := range artifact.Versions {
		archives := info.GetArchives()
		require.Len(t, archives, 1, "cri-o %s", version)

		var listed []string
		for _, b := range info.GetBinaries() {
			assert.Equal(t, "cri-o/bin/"+path.Base(b.Name), b.Name, "cri-o %s", version)
			assert.Equal(t, archives[0].Name, b.Archive, "cri-o %s %s", version, b.Name)
			listed = append(listed, path.Base(b.Name))
		}
		sort.Strings(listed)
		assert.Equal(t, layout, listed, "cri-o %s", version)
	}
}

func TestCatalog_KubernetesDownloadURLs(t *testing.T) {
	catalog, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for _, arch := range []string{"amd64", "arm64"} {
		data := func(v string) TemplateData { return TemplateData{VERSION: v, OS: "linux", ARCH: arch} }

		crio, err := catalog.GetHostArtifact(CrioArtifactName)
		require.NoError(t, err)
		archive := crio.Versions["1.36.7"].GetArchives()[0]
		url, err := executeTemplate(archive.URL, data("1.36.7"))
		require.NoError(t, err)
		assert.Equal(t, "https://storage.googleapis.com/cri-o/artifacts/cri-o."+arch+".v1.36.7.tar.gz", url)

		for _, name := range []string{"kubelet", "kubeadm", "kubectl"} {
			artifact, err := catalog.GetHostArtifact(name)
			require.NoError(t, err)
			binaries := artifact.Versions["1.36.5"].GetBinaries()
			require.Len(t, binaries, 1)
			url, err := executeTemplate(binaries[0].URL, data("1.36.5"))
			require.NoError(t, err)
			assert.Equal(t, "https://dl.k8s.io/release/v1.36.5/bin/linux/"+arch+"/"+name, url)
		}
	}
}

func renderKubeadmInit(t *testing.T, version string) string {
	t.Helper()
	out, err := templates.Render("files/kubeadm/kubeadm-init.yaml", templates.KubeadmInitData{
		KubeBootstrapToken: "abcdef.0123456789abcdef",
		SandboxDir:         "/opt/solo/weaver/sandbox",
		MachineIP:          "10.0.0.5",
		Hostname:           "node-1",
		KubernetesVersion:  version,
		PodSubnet:          "10.244.0.0/16",
	})
	require.NoError(t, err)
	return out
}

func TestKubeadmInstaller_FreshInstallRendersCatalogDefault(t *testing.T) {
	installer, err := NewKubeadmInstaller()
	require.NoError(t, err)
	require.Equal(t, "1.36.5", installer.Version())

	fresh := renderKubeadmInit(t, installer.Version())
	assert.Contains(t, fresh, "\nkubernetesVersion: 1.36.5\n")

	// The template itself is unchanged: only the version line differs from
	// what a 1.33.4 cluster was initialised with.
	existing := renderKubeadmInit(t, "1.33.4")
	assert.Equal(t, strings.Replace(existing, "kubernetesVersion: 1.33.4", "kubernetesVersion: 1.36.5", 1), fresh)
}

func TestKubeadmInstaller_ExplicitVersionIsKept(t *testing.T) {
	installer, err := NewKubeadmInstaller(WithVersion("1.33.4"))
	require.NoError(t, err)
	assert.Equal(t, "1.33.4", installer.Version())
}
