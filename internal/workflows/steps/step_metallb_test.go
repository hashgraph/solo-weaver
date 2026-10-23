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

	"github.com/hashgraph/solo-weaver/pkg/helm"
)

func managerFor(hm helm.Manager) func() (helm.Manager, error) {
	return func() (helm.Manager, error) { return hm, nil }
}

func TestMetalLBHelmValues_DisableBothBGPBackends(t *testing.T) {
	assert.ElementsMatch(t, []string{"speaker.frr.enabled=false", "frrk8s.enabled=false"}, metalLBHelmValues())
}

func TestInstallMetalLB_FreshInstallUsesDefaultChartAndIsRolledBack(t *testing.T) {
	spec := chartSpec("metallb")
	require.Equal(t, "0.16.1", spec.Version)
	const localChart = "/tmp/charts/metallb-0.16.1.tgz"

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, "0.16.1", "sha256", spec.Checksum).Return(localChart, nil)
	hm.EXPECT().
		InstallChart(gomock.Any(), spec.Release, localChart, "", spec.Namespace, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.InstallChartOptions) (*release.Release, error) {
			require.NotNil(t, opts.ValueOpts)
			assert.Equal(t, metalLBHelmValues(), opts.ValueOpts.Values)
			assert.True(t, opts.Atomic)
			assert.True(t, opts.Wait)
			return nil, nil
		})
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)

	step, err := installMetalLB(managerFor(hm)).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, "true", report.Metadata[InstalledByThisStep])

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
}

// An existing release, whatever chart version it runs, is left alone, and its
// rollback must not uninstall it.
func TestInstallMetalLB_ExistingReleaseIsSkippedAndNotRolledBack(t *testing.T) {
	spec := chartSpec("metallb")
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)

	step, err := installMetalLB(managerFor(hm)).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, "true", report.Metadata[AlreadyInstalled])

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
}

func TestInstallMetalLB_ReleaseLookupErrorInstallsNothing(t *testing.T) {
	spec := chartSpec("metallb")
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, errors.New("cluster unreachable"))

	step, err := installMetalLB(managerFor(hm)).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)

	rollback := step.Rollback(context.Background())
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
}
