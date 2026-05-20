// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/registry"
)

// ErrChecksumMismatch is returned when a pulled chart's digest does not match
// the value recorded in the infrastructure catalog.
var ErrChecksumMismatch = ErrNamespace.NewType("checksum_mismatch")

const sha256Algorithm = "sha256"

// PullAndVerify implements Manager.PullAndVerify. It pulls the chart into
// destDir, computes the artifact integrity value appropriate for the chart's
// distribution mode, and compares against the expected checksum supplied from
// the catalog.
//
// For classic charts the algorithm is SHA256 of the resulting .tgz on disk.
// For OCI charts the algorithm is the manifest digest reported by the Helm
// registry client (which is itself a sha256 of the manifest descriptor).
// In both cases the catalog stores a bare hex string (no "sha256:" prefix),
// so the OCI path strips the prefix before comparison.
//
// destDir is created on demand (mode 0o755). The caller decides where the
// pulled .tgz lives — workflow steps point at <WeaverPaths.DownloadsDir>/charts;
// unit tests can pass `t.TempDir()`.
//
// Context cancellation: ctx is accepted for API symmetry with the rest of
// helm.Manager but is not propagated into the pull itself. Neither
// action.Pull.Run nor registry.Client.Pull accept a context in the Helm SDK
// version we vendor, so an in-flight pull cannot be cancelled — the call
// runs to completion (success or timeout) regardless of ctx. Workflow steps
// remain cancellable between catalog lookups and the pull call, but a slow
// pull will block until the underlying HTTP transport's own deadlines fire.
func (h *helmManager) PullAndVerify(ctx context.Context, destDir, chartRef, version, algorithm, expectedChecksum string) (string, error) {
	_ = ctx // intentionally unused; see context-cancellation note above
	if algorithm != sha256Algorithm {
		return "", errorx.IllegalArgument.New(
			"unsupported checksum algorithm %q for chart %q (only %q is supported)",
			algorithm, chartRef, sha256Algorithm)
	}
	if expectedChecksum == "" {
		return "", errorx.IllegalArgument.New("expected checksum is empty for chart %q version %q", chartRef, version)
	}
	if destDir == "" {
		return "", errorx.IllegalArgument.New("destDir is empty for chart %q version %q", chartRef, version)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to create chart downloads dir %q", destDir)
	}

	if registry.IsOCI(chartRef) {
		return h.pullAndVerifyOCI(destDir, chartRef, version, expectedChecksum)
	}
	return h.pullAndVerifyClassic(destDir, chartRef, version, expectedChecksum)
}

// pullAndVerifyClassic uses action.Pull (the SDK equivalent of `helm pull`)
// to fetch a chart from a classic repo into destDir and compares the SHA256
// of the resulting .tgz to the catalog-recorded value. The repository alias
// in chartRef (e.g. "metallb/metallb") must have been registered via AddRepo
// before this call so the repository config can resolve the URL.
func (h *helmManager) pullAndVerifyClassic(destDir, chartRef, version, expected string) (string, error) {
	settings := h.WithNamespace("")

	registryClient, err := newRegistryClient(settings)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to create registry client")
	}

	actionConfig, err := initActionConfig(settings, h.log.Printf)
	if err != nil {
		return "", errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}
	actionConfig.RegistryClient = registryClient

	pull := action.NewPullWithOpts(action.WithConfig(actionConfig))
	pull.Settings = settings
	pull.DestDir = destDir
	pull.Untar = false
	pull.Version = version
	pull.SetRegistryClient(registryClient)

	if _, err := pull.Run(chartRef); err != nil {
		return "", ErrChartLoadFailed.Wrap(err, "helm pull failed for chart %q version %q", chartRef, version)
	}

	// `helm pull <repo>/<chart>` writes <chart>-<version>.tgz (using the last
	// path segment of the chart reference). Mirrors scripts/chart-checksums/main.go.
	tgz := filepath.Join(destDir, fmt.Sprintf("%s-%s.tgz", path.Base(chartRef), version))
	actual, err := sha256File(tgz)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to compute SHA256 of pulled chart %q", tgz)
	}
	if !strings.EqualFold(actual, expected) {
		return "", ErrChecksumMismatch.New(
			"chart %q version %q checksum mismatch: expected sha256=%s, got sha256=%s",
			chartRef, version, expected, actual)
	}

	h.log.Info().
		Str("chart", chartRef).
		Str("version", version).
		Str("algorithm", sha256Algorithm).
		Str("checksum", actual).
		Msg("Helm chart pulled and verified")

	return tgz, nil
}

// pullAndVerifyOCI calls the registry client directly so we can read the
// manifest digest from PullResult.Manifest.Digest (which is reported by
// `helm pull` as the "Digest: sha256:..." line). The chart layer bytes are
// then written to disk so callers can pass the local path to InstallChart.
func (h *helmManager) pullAndVerifyOCI(destDir, chartRef, version, expected string) (string, error) {
	settings := h.WithNamespace("")

	registryClient, err := newRegistryClient(settings)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to create registry client")
	}

	// `helm pull oci://host/path/chart --version X` translates internally to
	// `registry.Client.Pull("host/path/chart:X")`. Reproduce that here.
	ref := fmt.Sprintf("%s:%s", strings.TrimPrefix(chartRef, "oci://"), version)
	result, err := registryClient.Pull(ref, registry.PullOptWithChart(true))
	if err != nil {
		return "", ErrChartLoadFailed.Wrap(err, "helm pull failed for OCI chart %q version %q", chartRef, version)
	}
	if result == nil || result.Manifest == nil || result.Chart == nil {
		return "", ErrChartLoadFailed.New("helm pull returned incomplete result for OCI chart %q version %q", chartRef, version)
	}

	actual := strings.TrimPrefix(result.Manifest.Digest, "sha256:")
	if !strings.EqualFold(actual, expected) {
		return "", ErrChecksumMismatch.New(
			"chart %q version %q manifest digest mismatch: expected sha256=%s, got sha256=%s",
			chartRef, version, expected, actual)
	}

	tgz := filepath.Join(destDir, fmt.Sprintf("%s-%s.tgz", path.Base(chartRef), version))
	if err := os.WriteFile(tgz, result.Chart.Data, 0o600); err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to write pulled OCI chart to %q", tgz)
	}

	h.log.Info().
		Str("chart", chartRef).
		Str("version", version).
		Str("algorithm", sha256Algorithm).
		Str("checksum", actual).
		Msg("Helm chart pulled and verified")

	return tgz, nil
}

// sha256File returns the lowercase hex SHA256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
