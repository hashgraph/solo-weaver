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

	archive, err := chartutil.Save(&chart.Chart{
		Metadata:  &chart.Metadata{APIVersion: chart.APIVersionV2, Name: "cilium", Version: version, AppVersion: version},
		Templates: []*chart.File{{Name: "templates/configmap.yaml", Data: []byte("kind: ConfigMap\n")}},
	}, repoDir)
	require.NoError(t, err)

	index, err := repo.IndexDirectory(repoDir, srv.URL)
	require.NoError(t, err)
	require.NoError(t, index.WriteFile(filepath.Join(repoDir, "index.yaml"), 0o644))

	data, err := os.ReadFile(archive)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return ciliumRelease{Version: version, ChartRepo: srv.URL, ChartSHA256: hex.EncodeToString(sum[:])}, srv
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

func TestPullVerifiedCiliumChart_VerifiedChartIsUnpacked(t *testing.T) {
	release, _ := ciliumChartRepo(t, "1.20.2")
	dest := t.TempDir()

	dir, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, dest)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dest, "cilium-1.20.2", "cilium"), dir)
	chartYAML, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(chartYAML), "version: 1.20.2")
}

func TestPullVerifiedCiliumChart_ReplacesAStaleUnpackedChart(t *testing.T) {
	release, _ := ciliumChartRepo(t, "1.20.2")
	dest := t.TempDir()
	stale := filepath.Join(dest, "cilium-1.20.2", "cilium", "templates", "injected.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
	require.NoError(t, os.WriteFile(stale, []byte("kind: Secret\n"), 0o644))

	_, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, dest)

	require.NoError(t, err)
	assert.NoFileExists(t, stale, "only files from the verified archive may be installed")
}

func TestPullVerifiedCiliumChart_ChecksumMismatchIsNotUnpacked(t *testing.T) {
	release, _ := ciliumChartRepo(t, "1.20.2")
	release.ChartSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	dest := t.TempDir()

	dir, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, dest)

	require.Error(t, err)
	assert.Empty(t, dir)
	assert.Contains(t, err.Error(), "failed verification, so 'cilium install' was not run")
	assertReason(t, err, reasons.IntegrityCheckFailed)
	assert.NoDirExists(t, filepath.Join(dest, "cilium-1.20.2"))
}

func TestPullVerifiedCiliumChart_DownloadFailure(t *testing.T) {
	t.Run("chart missing from the repository", func(t *testing.T) {
		release, _ := ciliumChartRepo(t, "1.20.1")
		release.Version = "1.20.2"
		dest := t.TempDir()

		dir, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, dest)

		require.Error(t, err)
		assert.Empty(t, dir)
		assertReason(t, err, reasons.DownloadFailed)
		assert.NoDirExists(t, filepath.Join(dest, "cilium-1.20.2"))
	})

	t.Run("repository unreachable", func(t *testing.T) {
		release, srv := ciliumChartRepo(t, "1.20.2")
		srv.Close()

		dir, err := pullVerifiedCiliumChart(context.Background(), isolatedHelm(t), release, t.TempDir())

		require.Error(t, err)
		assert.Empty(t, dir)
		assertReason(t, err, reasons.DownloadFailed)
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
