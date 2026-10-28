// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

func serverVersionOf(v string) serverVersionFunc {
	return func() (string, error) { return v, nil }
}

func noServerVersion(t *testing.T) serverVersionFunc {
	return func() (string, error) {
		t.Fatal("the API server version must not be read")
		return "", nil
	}
}

func TestMetricsServerChartFor(t *testing.T) {
	cases := map[string]string{
		"v1.36.5":       "3.14.0",
		"v1.34.0":       "3.14.0",
		"v1.37.1":       "3.14.0",
		"v1.33.4":       "3.13.0",
		"v1.33.13":      "3.13.0",
		"v1.34.0-rc.1":  "3.13.0",
		"1.31.0":        "3.13.0",
		"v1.36.5+build": "3.14.0",
	}
	for kubeVersion, want := range cases {
		t.Run(kubeVersion, func(t *testing.T) {
			spec, err := metricsServerChartFor(kubeVersion)
			require.NoError(t, err)
			assert.Equal(t, want, spec.Version)
			expected, err := resolveCatalogChartVersion("metrics-server", want)
			require.NoError(t, err)
			assert.Equal(t, expected.Checksum, spec.Checksum)
		})
	}
}

func TestMetricsServerChartFor_UnusableVersionIsRefused(t *testing.T) {
	for _, kubeVersion := range []string{"", "  ", "not-a-version", "v1.x.3"} {
		t.Run(kubeVersion, func(t *testing.T) {
			_, err := metricsServerChartFor(kubeVersion)
			require.Error(t, err)
		})
	}
}

// expectInstall sets up a full install of the metrics-server chart at version.
func expectInstall(t *testing.T, hm *helm.MockManager, version string) {
	t.Helper()
	spec, err := resolveCatalogChartVersion("metrics-server", version)
	require.NoError(t, err)
	localChart := "/tmp/charts/metrics-server-" + version + ".tgz"

	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, version, "sha256", spec.Checksum).Return(localChart, nil)
	hm.EXPECT().
		InstallChart(gomock.Any(), spec.Release, localChart, "", spec.Namespace, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.InstallChartOptions) (*release.Release, error) {
			require.NotNil(t, opts.ValueOpts)
			assert.Equal(t, defaultMetricsServerValues().Values, opts.ValueOpts.Values)
			assert.True(t, opts.Atomic)
			return nil, nil
		})
}

func TestInstallMetricsServer_FreshClusterGetsDefaultChartAndIsRolledBack(t *testing.T) {
	spec := chartSpec("metrics-server")
	require.Equal(t, "3.14.0", spec.Version)

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	expectInstall(t, hm, "3.14.0")
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)

	step, err := installMetricsServer(nil, managerFor(hm), serverVersionOf("v1.36.5")).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, "true", report.Metadata[InstalledByThisStep])
	assert.Equal(t, "3.14.0", report.Metadata["chartVersion"])

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
}

// An existing release is left alone without asking for the cluster version,
// and its rollback must not uninstall it.
func TestInstallMetricsServer_ExistingReleaseIsSkippedAndNotRolledBack(t *testing.T) {
	spec := chartSpec("metrics-server")
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)

	step, err := installMetricsServer(nil, managerFor(hm), noServerVersion(t)).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, "true", report.Metadata[AlreadyInstalled])

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
}

// A Kubernetes 1.33 cluster missing its release gets back the 3.13.0 chart it
// was built with, not a chart that needs 1.34.
func TestInstallMetricsServer_OlderClusterMissingReleaseGetsLegacyChart(t *testing.T) {
	spec := chartSpec("metrics-server")
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	expectInstall(t, hm, "3.13.0")

	step, err := installMetricsServer(nil, managerFor(hm), serverVersionOf("v1.33.4")).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, "3.13.0", report.Metadata["chartVersion"])
}

// A cluster whose version cannot be read or used installs nothing: gomock fails
// the test on any AddRepo, PullAndVerify or InstallChart call.
func TestInstallMetricsServer_UnknownClusterVersionInstallsNothing(t *testing.T) {
	cases := map[string]struct {
		serverVersion serverVersionFunc
		reason        errx.Reason
	}{
		"API unreachable": {func() (string, error) { return "", errors.New("connection refused") }, reasons.K8sAPIFailed},
		"empty version":   {serverVersionOf(""), reasons.PreconditionNotMet},
		"garbage version": {serverVersionOf("not-a-version"), reasons.PreconditionNotMet},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec := chartSpec("metrics-server")
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)

			step, err := installMetricsServer(nil, managerFor(hm), tc.serverVersion).Build()
			require.NoError(t, err)

			report := step.Execute(context.Background())
			require.Error(t, report.Error)
			assertReason(t, report.Error, tc.reason)

			rollback := step.Rollback(context.Background())
			assert.Equal(t, automa.StatusSkipped, rollback.Status)
		})
	}
}

func TestInstallMetricsServer_ReleaseLookupErrorInstallsNothing(t *testing.T) {
	spec := chartSpec("metrics-server")
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, errors.New("cluster unreachable"))

	step, err := installMetricsServer(nil, managerFor(hm), noServerVersion(t)).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
}
