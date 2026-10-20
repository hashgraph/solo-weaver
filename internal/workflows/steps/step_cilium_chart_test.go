// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/repo"

	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// ciliumChartRepo serves a stand-in cilium chart from a local Helm repository
// and returns a release pointing at it, with the archive's real sha256.
func ciliumChartRepo(t *testing.T, version string) (ciliumRelease, *httptest.Server) {
	t.Helper()
	repoDir := t.TempDir()
	srv := httptest.NewServer(http.FileServer(http.Dir(repoDir)))
	t.Cleanup(srv.Close)

	archive := saveChart(t, repoDir, "cilium", version)

	index, err := repo.IndexDirectory(repoDir, srv.URL)
	require.NoError(t, err)
	require.NoError(t, index.WriteFile(filepath.Join(repoDir, "index.yaml"), 0o644))

	return ciliumRelease{Version: version, ChartRepo: srv.URL, ChartSHA256: fileSHA256(t, archive)}, srv
}

func saveChart(t *testing.T, dir, name, version string) string {
	t.Helper()
	archive, err := chartutil.Save(&chart.Chart{
		Metadata:  &chart.Metadata{APIVersion: chart.APIVersionV2, Name: name, Version: version, AppVersion: version},
		Templates: []*chart.File{{Name: "templates/configmap.yaml", Data: []byte("kind: ConfigMap\n")}},
	}, dir)
	require.NoError(t, err)
	return archive
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(des))
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

// isolatedHelm returns a real helm manager whose repository config and cache
// live in a temp dir.
func isolatedHelm(t *testing.T) helm.Manager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HELM_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("HELM_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("HELM_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(home, "config", "repositories.yaml"))
	t.Setenv("HELM_REPOSITORY_CACHE", filepath.Join(home, "cache", "repository"))
	hm, err := helm.NewManager(helm.WithLogger(*logx.As()))
	require.NoError(t, err)
	return hm
}

func TestPullVerifiedCiliumChart_VerifiedChartIsUnpackedInAPrivateDirectory(t *testing.T) {
	release, _ := ciliumChartRepo(t, "1.20.2")
	base := t.TempDir()

	dir, cleanup, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, base)

	require.NoError(t, err)
	workDir := filepath.Dir(dir)
	assert.Equal(t, base, filepath.Dir(workDir))
	assert.Equal(t, "cilium", filepath.Base(dir))
	fi, err := os.Stat(workDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
	chartYAML, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(chartYAML), "version: 1.20.2")

	cleanup()
	assert.Empty(t, entries(t, base), "cleanup must remove the work directory")
}

func TestPullVerifiedCiliumChart_EachCallUsesAFreshDirectory(t *testing.T) {
	release, _ := ciliumChartRepo(t, "1.20.2")
	base := t.TempDir()
	hm := isolatedHelm(t)

	first, cleanupFirst, err := pullVerifiedCiliumChart(context.Background(), hm, release, base)
	require.NoError(t, err)
	t.Cleanup(cleanupFirst)
	second, cleanupSecond, err := pullVerifiedCiliumChart(context.Background(), hm, release, base)
	require.NoError(t, err)
	t.Cleanup(cleanupSecond)

	assert.NotEqual(t, first, second)
}

func TestPullVerifiedCiliumChart_ChecksumMismatchLeavesNothing(t *testing.T) {
	release, _ := ciliumChartRepo(t, "1.20.2")
	release.ChartSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	base := t.TempDir()

	dir, cleanup, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, base)

	require.Error(t, err)
	assert.Empty(t, dir)
	assert.Nil(t, cleanup)
	assert.Contains(t, err.Error(), "failed verification, so 'cilium install' was not run")
	assertReason(t, err, reasons.IntegrityCheckFailed)
	assert.Empty(t, entries(t, base))
}

func TestPullVerifiedCiliumChart_DownloadFailure(t *testing.T) {
	t.Run("chart missing from the repository", func(t *testing.T) {
		release, _ := ciliumChartRepo(t, "1.20.1")
		release.Version = "1.20.2"
		base := t.TempDir()

		dir, _, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, base)

		require.Error(t, err)
		assert.Empty(t, dir)
		assertReason(t, err, reasons.DownloadFailed)
		assert.Empty(t, entries(t, base))
	})

	t.Run("repository unreachable", func(t *testing.T) {
		release, srv := ciliumChartRepo(t, "1.20.2")
		srv.Close()
		base := t.TempDir()

		dir, _, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, base)

		require.Error(t, err)
		assert.Empty(t, dir)
		assertReason(t, err, reasons.DownloadFailed)
		assert.Empty(t, entries(t, base))
	})
}

func TestUnpackVerifiedChart_UnpacksOnlyTheBytesItChecked(t *testing.T) {
	src := t.TempDir()
	verified := saveChart(t, src, "cilium", "1.20.2")
	release := ciliumRelease{Version: "1.20.2", ChartSHA256: fileSHA256(t, verified)}

	// Another archive now sits where the verified one was.
	other := saveChart(t, t.TempDir(), "cilium", "1.20.2-tampered")
	data, err := os.ReadFile(other)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(verified, data, 0o600))
	dest := t.TempDir()

	dir, err := unpackVerifiedChart(verified, release, dest)

	require.Error(t, err)
	assert.Empty(t, dir)
	assertReason(t, err, reasons.IntegrityCheckFailed)
	assert.Empty(t, entries(t, dest))
}

func TestUnpackVerifiedChart_ChartMustNameTheRelease(t *testing.T) {
	for name, chartID := range map[string][2]string{
		"other chart name": {"not-cilium", "1.20.2"},
		"other version":    {"cilium", "1.20.1"},
	} {
		t.Run(name, func(t *testing.T) {
			archive := saveChart(t, t.TempDir(), chartID[0], chartID[1])
			release := ciliumRelease{Version: "1.20.2", ChartSHA256: fileSHA256(t, archive)}
			dest := t.TempDir()

			dir, err := unpackVerifiedChart(archive, release, dest)

			require.Error(t, err)
			assert.Empty(t, dir)
			assert.Contains(t, err.Error(), "expected cilium 1.20.2")
			assertReason(t, err, reasons.IntegrityCheckFailed)
			assert.Empty(t, entries(t, dest))
		})
	}
}

func TestPrivateWorkDir(t *testing.T) {
	t.Run("private directory under a safe base", func(t *testing.T) {
		base := t.TempDir()
		dir, err := privateWorkDir(base, "w-")
		require.NoError(t, err)
		fi, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
	})

	t.Run("sticky world-writable base, like /tmp", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.Chmod(base, 0o777|os.ModeSticky))
		_, err := privateWorkDir(base, "w-")
		require.NoError(t, err)
	})

	t.Run("group-writable base without the sticky bit", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.Chmod(base, 0o775))
		dir, err := privateWorkDir(base, "w-")
		require.Error(t, err)
		assert.Empty(t, dir)
		assert.Contains(t, err.Error(), "without the sticky bit")
		assertReason(t, err, reasons.PreconditionNotMet)
		assert.Empty(t, entries(t, base), "a rejected work directory is removed")
	})

	t.Run("group-writable ancestor", func(t *testing.T) {
		top := t.TempDir()
		require.NoError(t, os.Chmod(top, 0o775))
		base := filepath.Join(top, "charts")
		require.NoError(t, os.Mkdir(base, 0o755))
		_, err := privateWorkDir(base, "w-")
		require.Error(t, err)
		assert.Contains(t, err.Error(), top)
	})
}

func TestNewClusterCilium_PinsTheChart(t *testing.T) {
	assert.Equal(t, ciliumRelease{
		Version:     "1.20.2",
		ChartRepo:   "https://helm.cilium.io",
		ChartSHA256: "b2afd87b7f75f875f92a14559f14f59b7babbb479d968e3fd625a20bf30ec20e",
	}, newClusterCilium)
}

func assertReason(t *testing.T, err error, want errx.Reason) {
	t.Helper()
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "error carries no reason: %v", err)
	assert.Equal(t, want, reason)
	hints, _ := errx.Hints(err)
	assert.NotEmpty(t, hints)
}
