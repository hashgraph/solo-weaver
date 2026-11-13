// SPDX-License-Identifier: Apache-2.0

package software

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func sha512Hex(data []byte) string {
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// checksumFixture serves, per arch, an archive holding a binary and a config,
// and a binary downloaded directly; plus one config downloaded by URL.
type checksumFixture struct {
	server   *httptest.Server
	archives map[string][]byte
	direct   map[string][]byte
	config   []byte
}

func newChecksumFixture(t *testing.T, archiveFiles map[string]map[string]string) *checksumFixture {
	t.Helper()
	f := &checksumFixture{
		archives: map[string][]byte{},
		direct:   map[string][]byte{"amd64": []byte("cli-amd64"), "arm64": []byte("cli-arm64")},
		config:   []byte("unit=1\n"),
	}
	for arch, files := range archiveFiles {
		f.archives[arch] = tarGz(t, files)
	}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/tool-") && strings.HasSuffix(p, ".tar.gz"):
			arch := strings.TrimSuffix(strings.TrimPrefix(p, "/tool-"), ".tar.gz")
			if data, ok := f.archives[arch]; ok {
				_, _ = w.Write(data)
				return
			}
		case strings.HasPrefix(p, "/cli/"):
			if data, ok := f.direct[strings.TrimPrefix(p, "/cli/")]; ok {
				_, _ = w.Write(data)
				return
			}
		case p == "/tool.conf":
			_, _ = w.Write(f.config)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func emptyChecksums(algorithm string) PlatformChecksum {
	return PlatformChecksum{"linux": {
		"amd64": {Algorithm: algorithm},
		"arm64": {Algorithm: algorithm},
	}}
}

func (f *checksumFixture) artifact(binaryAlgorithm string) ArtifactMetadata {
	return ArtifactMetadata{
		Name: "tool",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Archives: []ArchiveDetail{{
					Name:             "tool-{{.ARCH}}.tar.gz",
					URL:              f.server.URL + "/tool-{{.ARCH}}.tar.gz",
					PlatformChecksum: emptyChecksums("sha256"),
				}},
				Binaries: []BinaryDetail{
					{Name: "tool/bin/tool", Archive: "tool-{{.ARCH}}.tar.gz", PlatformChecksum: emptyChecksums(binaryAlgorithm)},
					{Name: "cli", URL: f.server.URL + "/cli/{{.ARCH}}", PlatformChecksum: emptyChecksums("sha512")},
				},
				Configs: []ConfigDetail{
					{Name: "tool.conf", URL: f.server.URL + "/tool.conf", Algorithm: "sha512"},
					{Name: "tool/etc/tool.service", Archive: "tool-{{.ARCH}}.tar.gz", Algorithm: "sha256"},
				},
			},
		},
	}
}

func (f *checksumFixture) installer(t *testing.T, artifact ArtifactMetadata, arch string) *baseInstaller {
	t.Helper()
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)
	workDir := t.TempDir()
	return &baseInstaller{
		name:                 artifact.Name,
		software:             artifact.withPlatform("linux", arch),
		versionToBeInstalled: "1.0.0",
		fileManager:          fsxManager,
		workDir:              workDir,
		downloader: NewDownloader(
			WithHTTPClient(f.server.Client()),
			WithAllowedDomains([]string{"127.0.0.1", "localhost"}),
			WithBasePath(workDir),
			WithRetryDelay(0),
		),
	}
}

func archiveFiles() map[string]map[string]string {
	return map[string]map[string]string{
		"amd64": {"tool/bin/tool": "tool-amd64", "tool/etc/tool.service": "[Unit]\n"},
		"arm64": {"tool/bin/tool": "tool-arm64", "tool/etc/tool.service": "[Unit]\n"},
	}
}

func TestComputeChecksums_HashesEveryFileOfAPlatform(t *testing.T) {
	f := newChecksumFixture(t, archiveFiles())

	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			got, err := f.installer(t, f.artifact("sha256"), arch).computeChecksums()
			require.NoError(t, err)

			require.Equal(t, []ComputedChecksum{
				{Section: SectionArchives, Name: "tool-{{.ARCH}}.tar.gz", Algorithm: "sha256", Value: calculateSHA256(f.archives[arch])},
				{Section: SectionBinaries, Name: "cli", Algorithm: "sha512", Value: sha512Hex(f.direct[arch])},
				{Section: SectionConfigs, Name: "tool.conf", Algorithm: "sha512", Value: sha512Hex(f.config)},
				{Section: SectionBinaries, Name: "tool/bin/tool", Algorithm: "sha256", Value: calculateSHA256([]byte("tool-" + arch))},
				{Section: SectionConfigs, Name: "tool/etc/tool.service", Algorithm: "sha256", Value: calculateSHA256([]byte("[Unit]\n"))},
			}, got)
		})
	}
}

func TestComputeChecksums_MissingBinaryInArchive(t *testing.T) {
	files := archiveFiles()
	delete(files["amd64"], "tool/bin/tool")
	f := newChecksumFixture(t, files)

	_, err := f.installer(t, f.artifact("sha256"), "amd64").computeChecksums()
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool/bin/tool")
}

func TestComputeChecksums_UnsupportedAlgorithm(t *testing.T) {
	f := newChecksumFixture(t, archiveFiles())

	_, err := f.installer(t, f.artifact(""), "amd64").computeChecksums()
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, errorx.IllegalFormat), err.Error())
}

func TestComputeChecksums_PlatformMissingFromCatalog(t *testing.T) {
	f := newChecksumFixture(t, archiveFiles())
	artifact := f.artifact("sha256")
	details := artifact.Versions["1.0.0"]
	details.Binaries[0].PlatformChecksum = PlatformChecksum{"linux": {"amd64": {Algorithm: "sha256"}}}
	artifact.Versions["1.0.0"] = details

	_, err := f.installer(t, artifact, "arm64").computeChecksums()
	require.Error(t, err)
	require.Contains(t, err.Error(), "arm64")
}
