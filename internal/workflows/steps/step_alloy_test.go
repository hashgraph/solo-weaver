// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/pkg/helm"
)

func stepIds(t *testing.T, wb *automa.WorkflowBuilder) []string {
	t.Helper()
	stp, err := wb.Build()
	require.NoError(t, err)
	wf, ok := stp.(automa.Workflow)
	require.True(t, ok)
	var ids []string
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	return ids
}

// The release check runs in the preflight phase, before the CRD phase and
// before any Alloy resource is written.
func TestSetupAlloyStack_ChecksReleasesBeforeChangingAnything(t *testing.T) {
	assert.Equal(t, []string{PreflightAlloyPhaseId, SetupPrometheusCRDsStepId, SetupAlloyStepId}, stepIds(t, SetupAlloyStack()))
	assert.Equal(t, []string{PreCheckAlloyStepId, CheckAlloyReleasesStepId}, stepIds(t, PreflightAlloy()))
}

func TestCheckAlloyReleases(t *testing.T) {
	deployed := func(chartName, version string) *release.Release {
		return releaseOf(chartName, version, release.StatusDeployed)
	}
	for name, tc := range map[string]struct {
		alloy, crds, nodeExporter *release.Release
		ok                        bool
	}{
		"fresh cluster":              {ok: true},
		"cluster built by v0.34.0":   {alloy: deployed("alloy", "1.4.0"), crds: deployed("prometheus-operator-crds", "24.0.1"), nodeExporter: deployed("node-exporter", "4.5.19"), ok: true},
		"alloy chart not in catalog": {alloy: deployed("alloy", "1.9.0")},
		"alloy upgrade failed":       {alloy: releaseOf("alloy", "1.13.1", release.StatusFailed)},
	} {
		t.Run(name, func(t *testing.T) {
			// Only GetRelease is expected: gomock fails on any change.
			hm := helm.NewMockManager(gomock.NewController(t))
			for _, r := range []struct {
				name string
				rel  *release.Release
			}{{"alloy", tc.alloy}, {"prometheus-operator-crds", tc.crds}, {"node-exporter", tc.nodeExporter}} {
				var err error
				if r.rel == nil {
					err = notFound()
				}
				spec := chartSpec(r.name)
				hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(r.rel, err).MaxTimes(1)
			}

			step, err := checkAlloyReleases(managerFor(hm)).Build()
			require.NoError(t, err)
			report := step.Execute(context.Background())
			if tc.ok {
				require.NoError(t, report.Error)
			} else {
				require.Error(t, report.Error)
			}
		})
	}
}

// expectAlloyDeploy sets up the chart pull and deploy for Alloy at version and
// checks the values passed to Helm.
func expectAlloyDeploy(t *testing.T, hm *helm.MockManager, version string) {
	t.Helper()
	spec, err := resolveCatalogChartVersion("alloy", version)
	require.NoError(t, err)
	wantJSON, err := alloy.ChartJSONValues(version)
	require.NoError(t, err)
	localChart := "/tmp/charts/alloy-" + version + ".tgz"

	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, version, "sha256", spec.Checksum).Return(localChart, nil)
	hm.EXPECT().
		DeployChart(gomock.Any(), spec.Release, localChart, "", spec.Namespace, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.DeployChartOptions) (*release.Release, error) {
			require.NotNil(t, opts.ValueOpts)
			assert.Equal(t, alloy.BaseHelmValues(), opts.ValueOpts.Values)
			assert.Equal(t, wantJSON, opts.ValueOpts.JSONValues)
			assert.True(t, opts.Atomic)
			return nil, nil
		})
}

func TestInstallAlloy_FreshInstallGetsDefaultAndIsRolledBack(t *testing.T) {
	hm := helm.NewMockManager(gomock.NewController(t))
	expectRelease(hm, "alloy", nil, notFound())
	expectAlloyDeploy(t, hm, "1.4.0")
	spec := chartSpec("alloy")
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)

	step, err := installAlloy(managerFor(hm)).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, "true", report.Metadata[InstalledByThisStep])
	assert.Equal(t, "1.4.0", report.Metadata["chartVersion"])

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
}

// An existing release is reconfigured at the chart it runs, with that chart's
// values, and its rollback never uninstalls it.
func TestInstallAlloy_ExistingReleaseKeepsItsChart(t *testing.T) {
	for _, version := range []string{"1.4.0"} {
		t.Run(version, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, "alloy", releaseOf("alloy", version, release.StatusDeployed), nil)
			expectAlloyDeploy(t, hm, version)

			step, err := installAlloy(managerFor(hm)).Build()
			require.NoError(t, err)
			report := step.Execute(context.Background())
			require.NoError(t, report.Error)
			assert.Equal(t, version, report.Metadata["chartVersion"])
			assert.Empty(t, report.Metadata[InstalledByThisStep])

			rollback := step.Rollback(context.Background())
			assert.Equal(t, automa.StatusSkipped, rollback.Status)
		})
	}
}

// A release that cannot be resolved is not touched, and nothing is rolled back.
func TestInstallAlloy_UnresolvableReleaseIsNotTouched(t *testing.T) {
	for name, tc := range map[string]struct {
		rel *release.Release
		err error
	}{
		"unknown version": {rel: releaseOf("alloy", "1.9.0", release.StatusDeployed)},
		"failed":          {rel: releaseOf("alloy", "1.13.1", release.StatusFailed)},
		"pending":         {rel: releaseOf("alloy", "1.4.0", release.StatusPendingRollback)},
		"uninstalled":     {rel: releaseOf("alloy", "1.4.0", release.StatusUninstalled)},
		"lookup error":    {err: errors.New("connection refused")},
	} {
		t.Run(name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, "alloy", tc.rel, tc.err)

			step, err := installAlloy(managerFor(hm)).Build()
			require.NoError(t, err)
			report := step.Execute(context.Background())
			require.Error(t, report.Error)

			rollback := step.Rollback(context.Background())
			assert.Equal(t, automa.StatusSkipped, rollback.Status)
		})
	}
}
