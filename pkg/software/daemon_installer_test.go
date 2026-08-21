// SPDX-License-Identifier: Apache-2.0

package software

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

const (
	testDaemonVersion = "1.2.3"
	testDaemonOS      = "linux"
	testDaemonArch    = "amd64"
)

// daemonReleaseServer is a stand-in for the GitHub release endpoint. It records
// which asset paths were requested so a test can assert not just what was
// verified, but what was fetched at all.
type daemonReleaseServer struct {
	*httptest.Server
	mu        sync.Mutex
	requested []string
}

// newDaemonReleaseServer serves the given assets keyed by request path. A request
// for an unknown path is a 404, as the real release endpoint would be.
func newDaemonReleaseServer(t *testing.T, assets map[string][]byte) *daemonReleaseServer {
	t.Helper()

	rs := &daemonReleaseServer{}
	rs.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.requested = append(rs.requested, r.URL.Path)
		rs.mu.Unlock()

		body, ok := assets[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *daemonReleaseServer) paths() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.requested...)
}

// binaryAssetPath and checksumAssetPath mirror the real release asset layout that
// the catalog's URL template resolves to.
func binaryAssetPath(version string) string {
	return fmt.Sprintf("/v%s/solo-provisioner-daemon-%s-%s", version, testDaemonOS, testDaemonArch)
}

func checksumAssetPath(version string) string {
	return binaryAssetPath(version) + ".sha256"
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newDaemonTestInstaller wires a daemonInstaller against rs, with all paths
// re-rooted under a temp home so nothing touches the real /opt/solo/weaver.
// pinned is the value the link-time digest would carry ("" for a build that was
// not stamped), and requested is the version being installed.
func newDaemonTestInstaller(t *testing.T, rs *daemonReleaseServer, pinned, requested string) *daemonInstaller {
	t.Helper()

	restorePaths := models.SetPaths(t.TempDir())
	t.Cleanup(restorePaths)

	// pinnedDaemonDigest is a link-time global; swap it for the duration of the test.
	prevDigest := pinnedDaemonDigest
	pinnedDaemonDigest = pinned
	t.Cleanup(func() { pinnedDaemonDigest = prevDigest })

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	client := rs.Client()
	client.Timeout = 30 * time.Second

	item := ArtifactMetadata{
		Name:    DaemonBinaryName,
		Default: Version(testDaemonVersion),
		SelfRelease: &SelfReleaseSpec{
			URL:        rs.URL + "/v{{.VERSION}}/solo-provisioner-daemon-{{.OS}}-{{.ARCH}}",
			BinaryName: DaemonBinaryName,
		},
	}

	return &daemonInstaller{baseInstaller: &baseInstaller{
		name: DaemonBinaryName,
		// One attempt with no backoff: these tests assert the verification
		// outcome, and the retry loop would only make a rejection slower.
		downloader: NewDownloader(
			WithHTTPClient(client),
			WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
			WithMaxAttempts(1),
			WithRetryDelay(0),
		),
		software:             item.withPlatform(testDaemonOS, testDaemonArch),
		fileManager:          fileManager,
		versionToBeInstalled: requested,
	}}
}

// downloadedBinaryPath is where downloadSelfRelease is expected to leave the
// verified artifact.
func downloadedBinaryPath(version string) string {
	return path.Join(models.Paths().DownloadsDir, path.Base(binaryAssetPath(version)))
}

// Test_DaemonInstaller_Download_PinnedDigest_Match is the default install path: the
// requested version is this build's own, so the link-time digest applies and the
// published checksum asset is never fetched.
func Test_DaemonInstaller_Download_PinnedDigest_Match(t *testing.T) {
	//
	// Given
	//
	binary := []byte("solo-provisioner-daemon co-released artifact")
	// Only the co-released version carries a link-time digest, and that is the
	// version --daemon-version defaults to.
	rs := newDaemonReleaseServer(t, map[string][]byte{
		binaryAssetPath(ownVersion()): binary,
	})
	installer := newDaemonTestInstaller(t, rs, sha256Hex(binary), ownVersion())

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.NoError(t, err)

	got, err := os.ReadFile(downloadedBinaryPath(ownVersion()))
	require.NoError(t, err, "the verified binary must be left in the downloads dir")
	require.Equal(t, binary, got)

	require.NotContains(t, rs.paths(), checksumAssetPath(ownVersion()),
		"the checksum asset must not be fetched when the link-time digest applies")
}

// Test_DaemonInstaller_Download_PinnedDigest_Mismatch covers a tampered or
// substituted artifact: it is rejected and must not be left on disk, because
// Install() does not re-verify.
func Test_DaemonInstaller_Download_PinnedDigest_Mismatch(t *testing.T) {
	//
	// Given
	//
	rs := newDaemonReleaseServer(t, map[string][]byte{
		binaryAssetPath(ownVersion()): []byte("tampered artifact"),
	})
	installer := newDaemonTestInstaller(t, rs, sha256Hex([]byte("the real artifact")), ownVersion())

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err)

	_, statErr := os.Stat(downloadedBinaryPath(ownVersion()))
	require.True(t, os.IsNotExist(statErr),
		"a rejected artifact must not survive in the downloads dir")
}

// Test_DaemonInstaller_Download_PublishedChecksum covers an explicit
// --daemon-version other than this build's own: no link-time digest applies, so
// the release's published .sha256 asset is fetched and used instead.
func Test_DaemonInstaller_Download_PublishedChecksum(t *testing.T) {
	//
	// Given
	//
	binary := []byte("solo-provisioner-daemon 1.2.3 artifact")
	checksumAsset := fmt.Appendf(nil, "%s  solo-provisioner-daemon-%s-%s\n",
		sha256Hex(binary), testDaemonOS, testDaemonArch)

	rs := newDaemonReleaseServer(t, map[string][]byte{
		binaryAssetPath(testDaemonVersion):   binary,
		checksumAssetPath(testDaemonVersion): checksumAsset,
	})
	// A stamped digest is present but belongs to a different version, so it must
	// not be applied to this one.
	installer := newDaemonTestInstaller(t, rs, sha256Hex([]byte("some other version")), testDaemonVersion)

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.NoError(t, err)
	require.Contains(t, rs.paths(), checksumAssetPath(testDaemonVersion),
		"a version other than this build's own must be held to the published checksum")

	got, err := os.ReadFile(downloadedBinaryPath(testDaemonVersion))
	require.NoError(t, err)
	require.Equal(t, binary, got)

	_, statErr := os.Stat(path.Join(models.Paths().DownloadsDir, path.Base(checksumAssetPath(testDaemonVersion))))
	require.True(t, os.IsNotExist(statErr),
		"the consumed checksum asset must not be left in the downloads dir")
}

// Test_DaemonInstaller_Download_PublishedChecksum_Mismatch covers a published
// checksum that does not match the served binary.
func Test_DaemonInstaller_Download_PublishedChecksum_Mismatch(t *testing.T) {
	//
	// Given
	//
	checksumAsset := fmt.Appendf(nil, "%s  solo-provisioner-daemon\n", sha256Hex([]byte("a different artifact")))
	rs := newDaemonReleaseServer(t, map[string][]byte{
		binaryAssetPath(testDaemonVersion):   []byte("served artifact"),
		checksumAssetPath(testDaemonVersion): checksumAsset,
	})
	installer := newDaemonTestInstaller(t, rs, "", testDaemonVersion)

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err)
	_, statErr := os.Stat(downloadedBinaryPath(testDaemonVersion))
	require.True(t, os.IsNotExist(statErr))
}

// Test_DaemonInstaller_Download_MalformedChecksumAsset asserts the binary is not
// even fetched when the checksum asset cannot yield a digest — there would be
// nothing to hold it to.
func Test_DaemonInstaller_Download_MalformedChecksumAsset(t *testing.T) {
	//
	// Given
	//
	rs := newDaemonReleaseServer(t, map[string][]byte{
		binaryAssetPath(testDaemonVersion):   []byte("served artifact"),
		checksumAssetPath(testDaemonVersion): []byte("<html>404 not found</html>"),
	})
	installer := newDaemonTestInstaller(t, rs, "", testDaemonVersion)

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err)
	require.NotContains(t, rs.paths(), binaryAssetPath(testDaemonVersion),
		"the binary must not be downloaded before an expected digest is established")
}

// Test_DaemonInstaller_Download_MissingChecksumAsset covers a version whose
// release has no published checksum: nothing can verify it, so the download fails
// rather than installing an unchecked binary.
func Test_DaemonInstaller_Download_MissingChecksumAsset(t *testing.T) {
	//
	// Given
	//
	rs := newDaemonReleaseServer(t, map[string][]byte{
		binaryAssetPath(testDaemonVersion): []byte("served artifact"),
	})
	installer := newDaemonTestInstaller(t, rs, "", testDaemonVersion)

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err)
	_, statErr := os.Stat(downloadedBinaryPath(testDaemonVersion))
	require.True(t, os.IsNotExist(statErr))
}

// Test_PinnedDigestFor asserts the digest is scoped to the version it was
// computed from: applying it to any other version would attest to bytes it never
// saw.
func Test_PinnedDigestFor(t *testing.T) {
	prev := pinnedDaemonDigest
	t.Cleanup(func() { pinnedDaemonDigest = prev })

	digest := strings.Repeat("a", sha256HexLen)

	tests := []struct {
		name      string
		pinned    string
		requested string
		wantOK    bool
	}{
		{name: "co-released version", pinned: digest, requested: ownVersion(), wantOK: true},
		{name: "other version", pinned: digest, requested: "9.9.9", wantOK: false},
		{name: "unstamped build", pinned: "", requested: ownVersion(), wantOK: false},
		{name: "no version requested", pinned: digest, requested: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinnedDaemonDigest = tt.pinned

			got, ok := pinnedDigestFor(tt.requested)

			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.pinned, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

// Test_ParseChecksumAsset covers the shapes a published checksum asset can take,
// and the malformed bodies that must be rejected rather than compared against.
func Test_ParseChecksumAsset(t *testing.T) {
	digest := strings.Repeat("ab", sha256HexLen/2)

	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{name: "sha256sum line", content: digest + "  solo-provisioner-daemon-linux-amd64\n", want: digest},
		{name: "digest only", content: digest, want: digest},
		{name: "uppercase digest", content: strings.ToUpper(digest) + "  file\n", want: digest},
		{name: "leading whitespace", content: "  " + digest + "  file\n", want: digest},
		{name: "trailing blank lines", content: digest + "  file\n\n\n", want: digest},
		{name: "binary-mode marker", content: digest + " *file\n", want: digest},
		{name: "empty", content: "", wantErr: true},
		{name: "whitespace only", content: "   \n", wantErr: true},
		{name: "truncated digest", content: digest[:32] + "  file\n", wantErr: true},
		{name: "not hex", content: strings.Repeat("z", sha256HexLen) + "  file\n", wantErr: true},
		{name: "html error page", content: "<html>404 not found</html>", wantErr: true},
		{name: "filename first", content: "solo-provisioner-daemon " + digest, wantErr: true},
		// A multi-entry body must not resolve to its first line: the entry for
		// this artifact need not be the one listed first.
		{name: "two entries", content: digest + "  a\n" + strings.Repeat("cd", sha256HexLen/2) + "  b\n", wantErr: true},
		{name: "too many fields", content: digest + "  file  extra\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChecksumAsset([]byte(tt.content), "test.sha256")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
