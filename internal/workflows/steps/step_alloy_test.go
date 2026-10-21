// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
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
		"fresh cluster":                {ok: true},
		"cluster built by v0.34.0":     {alloy: deployed("alloy", "1.4.0"), crds: deployed("prometheus-operator-crds", "24.0.1"), nodeExporter: deployed("node-exporter", "4.5.19"), ok: true},
		"alloy chart not in catalog":   {alloy: deployed("alloy", "1.9.0")},
		"alloy upgrade failed":         {alloy: releaseOf("alloy", "1.13.1", release.StatusFailed)},
		"crds install pending":         {alloy: deployed("alloy", "1.4.0"), crds: releaseOf("prometheus-operator-crds", "32.0.1", release.StatusPendingInstall)},
		"node-exporter uninstalled":    {alloy: deployed("alloy", "1.4.0"), crds: deployed("prometheus-operator-crds", "24.0.1"), nodeExporter: releaseOf("node-exporter", "4.5.19", release.StatusUninstalled)},
		"node-exporter lookup refused": {alloy: deployed("alloy", "1.4.0"), crds: deployed("prometheus-operator-crds", "24.0.1"), nodeExporter: releaseOf("prometheus-node-exporter", "4.59.0", release.StatusPendingUpgrade)},
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

func TestInstallNodeExporter(t *testing.T) {
	spec := chartSpec("node-exporter")

	t.Run("an existing Bitnami release is left alone", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "node-exporter", releaseOf("node-exporter", "4.5.19", release.StatusDeployed), nil)
		step, err := installNodeExporter(managerFor(hm)).Build()
		require.NoError(t, err)
		report := step.Execute(context.Background())
		require.NoError(t, report.Error)
		assert.Equal(t, "true", report.Metadata[AlreadyInstalled])
		assert.Equal(t, automa.StatusSkipped, step.Rollback(context.Background()).Status)
	})
	t.Run("a failed release is refused", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "node-exporter", releaseOf("prometheus-node-exporter", "4.59.0", release.StatusFailed), nil)
		step, err := installNodeExporter(managerFor(hm)).Build()
		require.NoError(t, err)
		require.Error(t, step.Execute(context.Background()).Error)
	})
	t.Run("a fresh install is rolled back", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "node-exporter", nil, notFound())
		hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).Return("/tmp/ne.tgz", nil)
		hm.EXPECT().InstallChart(gomock.Any(), spec.Release, "/tmp/ne.tgz", "", spec.Namespace, gomock.Any()).Return(nil, nil)
		hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)
		step, err := installNodeExporter(managerFor(hm)).Build()
		require.NoError(t, err)
		require.NoError(t, step.Execute(context.Background()).Error)
		assert.Equal(t, automa.StatusSuccess, step.Rollback(context.Background()).Status)
	})
}

func TestInstallPrometheusOperatorCRDs(t *testing.T) {
	spec := chartSpec("prometheus-operator-crds")
	require.Equal(t, "24.0.1", spec.Version)

	t.Run("an existing release keeps its CRDs", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "prometheus-operator-crds", releaseOf("prometheus-operator-crds", "24.0.1", release.StatusDeployed), nil)
		step, err := installPrometheusOperatorCRDs(managerFor(hm)).Build()
		require.NoError(t, err)
		report := step.Execute(context.Background())
		require.NoError(t, report.Error)
		assert.Equal(t, "true", report.Metadata[AlreadyInstalled])
		assert.Equal(t, automa.StatusSkipped, step.Rollback(context.Background()).Status)
	})
	for _, status := range []release.Status{release.StatusFailed, release.StatusPendingInstall, release.StatusUninstalled} {
		t.Run(string(status)+" is refused", func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, "prometheus-operator-crds", releaseOf("prometheus-operator-crds", "32.0.1", status), nil)
			step, err := installPrometheusOperatorCRDs(managerFor(hm)).Build()
			require.NoError(t, err)
			require.Error(t, step.Execute(context.Background()).Error)
			assert.Equal(t, automa.StatusSkipped, step.Rollback(context.Background()).Status)
		})
	}
	t.Run("a fresh install gets the default and is rolled back", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "prometheus-operator-crds", nil, notFound())
		hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, "24.0.1", "sha256", spec.Checksum).Return("/tmp/crds.tgz", nil)
		hm.EXPECT().InstallChart(gomock.Any(), spec.Release, "/tmp/crds.tgz", "", spec.Namespace, gomock.Any()).Return(nil, nil)
		hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)
		step, err := installPrometheusOperatorCRDs(managerFor(hm)).Build()
		require.NoError(t, err)
		require.NoError(t, step.Execute(context.Background()).Error)
		assert.Equal(t, automa.StatusSuccess, step.Rollback(context.Background()).Status)
	})
}

// fakeApplier records manifest calls; exists answers ResourceExists.
type fakeApplier struct {
	exists    bool
	existsErr error
	applied   []string
	deleted   []string
}

func (f *fakeApplier) ResourceExists(context.Context, string, string, string, string) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeApplier) ApplyManifest(_ context.Context, p string) error {
	f.applied = append(f.applied, p)
	return nil
}

func (f *fakeApplier) DeleteManifest(_ context.Context, p string) error {
	f.deleted = append(f.deleted, p)
	return nil
}

func useTempPaths(t *testing.T) {
	t.Helper()
	restore := models.SetPaths(t.TempDir())
	t.Cleanup(restore)
	require.NoError(t, os.MkdirAll(models.Paths().TempDir, 0o755))
}

// A ConfigMap the run created is deleted on rollback; one that already existed
// is updated in place and never deleted.
func TestDeployAlloyConfig_RollbackDeletesOnlyWhatItCreated(t *testing.T) {
	for name, tc := range map[string]struct {
		exists      bool
		wantDeleted int
		wantStatus  automa.TypeStatus
	}{
		"created by this run": {exists: false, wantDeleted: 1, wantStatus: automa.StatusSuccess},
		"already existed":     {exists: true, wantDeleted: 0, wantStatus: automa.StatusSkipped},
	} {
		t.Run(name, func(t *testing.T) {
			useTempPaths(t)
			k := &fakeApplier{exists: tc.exists}
			step, err := deployAlloyConfig(func() (manifestApplier, error) { return k, nil }).Build()
			require.NoError(t, err)

			report := step.Execute(context.Background())
			require.NoError(t, report.Error)
			assert.Len(t, k.applied, 1)

			rollback := step.Rollback(context.Background())
			require.NoError(t, rollback.Error)
			assert.Equal(t, tc.wantStatus, rollback.Status)
			assert.Len(t, k.deleted, tc.wantDeleted)
		})
	}
}

func TestDeployAlloyConfig_ExistenceCheckErrorAppliesNothing(t *testing.T) {
	useTempPaths(t)
	k := &fakeApplier{existsErr: errors.New("forbidden")}
	step, err := deployAlloyConfig(func() (manifestApplier, error) { return k, nil }).Build()
	require.NoError(t, err)

	require.Error(t, step.Execute(context.Background()).Error)
	assert.Empty(t, k.applied)
	assert.Equal(t, automa.StatusSkipped, step.Rollback(context.Background()).Status)
}

func TestApplyManifestOwned(t *testing.T) {
	for _, exists := range []bool{false, true} {
		k := &fakeApplier{exists: exists}
		created, err := applyManifestOwned(context.Background(), k, "/tmp/sm.yaml", "monitoring.coreos.com/v1", "ServiceMonitor", "block-node", alloy.BlockNodeServiceMonitorName)
		require.NoError(t, err)
		assert.Equal(t, !exists, created)
		assert.Equal(t, []string{"/tmp/sm.yaml"}, k.applied)
	}
}
