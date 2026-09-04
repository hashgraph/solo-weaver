// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/pkg/helm"
)

func Test_installESOChart_FreshInstall(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	const localChart = "/tmp/charts/external-secrets.tgz"

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().
		PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).
		Return(localChart, nil)
	hm.EXPECT().
		InstallChart(gomock.Any(), spec.Release, localChart, "", spec.Namespace, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.InstallChartOptions) (*release.Release, error) {
			require.NotNil(t, opts.ValueOpts)
			assert.Contains(t, opts.ValueOpts.Values, "installCRDs=true")
			assert.Contains(t, opts.ValueOpts.Values, "webhook.port=9443")
			assert.True(t, opts.CreateNamespace)
			assert.True(t, opts.Atomic)
			assert.True(t, opts.Wait)
			assert.Equal(t, helm.DefaultTimeout, opts.Timeout)
			return nil, nil
		})

	installed, err := installESOChart(context.Background(), hm, spec)
	require.NoError(t, err)
	assert.True(t, installed)
}

func Test_installESOChart_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)

	installed, err := installESOChart(context.Background(), hm, spec)
	require.NoError(t, err)
	assert.False(t, installed)
}

func Test_installESOChart_NamespaceOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	const customNS = "my-eso"
	spec.Namespace = customNS

	const localChart = "/tmp/charts/external-secrets.tgz"

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, customNS).Return(false, nil)
	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().
		PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).
		Return(localChart, nil)
	hm.EXPECT().
		InstallChart(gomock.Any(), spec.Release, localChart, "", customNS, gomock.Any()).
		Return(nil, nil)

	installed, err := installESOChart(context.Background(), hm, spec)
	require.NoError(t, err)
	assert.True(t, installed)
}

func Test_uninstallESOChart_Uninstalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)

	uninstalled, err := uninstallESOChart(hm, spec)
	require.NoError(t, err)
	assert.True(t, uninstalled)
}

func Test_uninstallESOChart_Idempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	// No UninstallChart expectation: not-installed must skip.

	uninstalled, err := uninstallESOChart(hm, spec)
	require.NoError(t, err)
	assert.False(t, uninstalled)
}

func Test_uninstallESOChart_NamespaceOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	const customNS = "my-eso"
	spec.Namespace = customNS

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, customNS).Return(true, nil)
	hm.EXPECT().UninstallChart(spec.Release, customNS).Return(nil)

	uninstalled, err := uninstallESOChart(hm, spec)
	require.NoError(t, err)
	assert.True(t, uninstalled)
}

func Test_uninstallESOChart_IsInstalledError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	wantErr := errors.New("is-installed boom")
	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, wantErr)
	// No UninstallChart expectation: the IsInstalled error must short-circuit.

	uninstalled, err := uninstallESOChart(hm, spec)
	require.ErrorIs(t, err, wantErr)
	assert.False(t, uninstalled)
}

func Test_uninstallESOChart_UninstallChartError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	wantErr := errors.New("uninstall boom")
	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(wantErr)

	uninstalled, err := uninstallESOChart(hm, spec)
	require.ErrorIs(t, err, wantErr)
	assert.False(t, uninstalled)
}

func Test_checkClusterReachable_Reachable(t *testing.T) {
	require.NoError(t, checkClusterReachable(func() (bool, error) { return true, nil }))
}

func Test_checkClusterReachable_NotReachable(t *testing.T) {
	err := checkClusterReachable(func() (bool, error) { return false, nil })
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalState),
		"an absent cluster is an IllegalState, got %v", err)
	hints, ok := errx.Hints(err)
	require.True(t, ok, "the failure must carry operator hints")
	assert.Contains(t, hints, "  solo-provisioner kube cluster install")
}

// ClusterExists returns no error today; the branch is covered because the
// signature allows one.
func Test_checkClusterReachable_ProbeError(t *testing.T) {
	err := checkClusterReachable(func() (bool, error) {
		return false, errors.New("probe boom")
	})
	// errorx.Type.Wrap is opaque, so the cause is readable in the message but
	// not reachable through errors.Is.
	require.Error(t, err)
	assert.ErrorContains(t, err, "probe boom")
	assert.True(t, errorx.IsOfType(err, errorx.ExternalError),
		"a failed probe is an ExternalError, got %v", err)
}

func esoRelease(name, namespace, chartName string, status release.Status) *release.Release {
	return &release.Release{
		Name:      name,
		Namespace: namespace,
		Info:      &release.Info{Status: status},
		Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: chartName}},
	}
}

func Test_checkESOSingleton_NoExistingRelease(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().ListAll().Return(nil, nil)

	require.NoError(t, checkESOSingleton(hm, spec))
}

func Test_checkESOSingleton_DeployedInTargetNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().ListAll().Return([]*release.Release{
		esoRelease(spec.Release, spec.Namespace, esoChartName, release.StatusDeployed),
	}, nil)

	// The existing idempotent no-op in installESOChart handles this case.
	require.NoError(t, checkESOSingleton(hm, spec))
}

func Test_checkESOSingleton_DeployedInAnotherNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	spec.Namespace = "my-eso"

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().ListAll().Return([]*release.Release{
		esoRelease("eso", "eso-old", esoChartName, release.StatusDeployed),
	}, nil)

	err = checkESOSingleton(hm, spec)
	require.Error(t, err)
	// Distinct release and namespace, so a swapped format argument fails here.
	assert.Contains(t, err.Error(), `"eso-old"`, "the message must name the occupied namespace")
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	assert.Contains(t, hints, "  helm uninstall eso -n eso-old")
}

// A stalled release is invisible to IsInstalled, so installESOChart would treat
// it as absent and Helm would then reject the name collision.
func Test_checkESOSingleton_StalledInTargetNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().ListAll().Return([]*release.Release{
		esoRelease(spec.Release, spec.Namespace, esoChartName, release.StatusFailed),
	}, nil)

	err = checkESOSingleton(hm, spec)
	require.Error(t, err)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	// helm, not "eso operator uninstall": that command skips a non-deployed release.
	assert.Contains(t, hints, "  helm uninstall external-secrets -n external-secrets")
}

// Chart name is authoritative, so an unrelated chart that happens to be named
// "external-secrets" must not trip the guard.
func Test_checkESOSingleton_IgnoresUnrelatedChart(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	spec.Namespace = "my-eso"

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().ListAll().Return([]*release.Release{
		esoRelease("external-secrets", "somewhere-else", "some-other-chart", release.StatusDeployed),
	}, nil)

	require.NoError(t, checkESOSingleton(hm, spec))
}

func Test_checkESOSingleton_ListError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(ctrl)
	hm.EXPECT().ListAll().Return(nil, errors.New("list boom"))

	err = checkESOSingleton(hm, spec)
	// errorx.Type.Wrap is opaque, so the cause is readable in the message but
	// not reachable through errors.Is.
	require.Error(t, err)
	assert.ErrorContains(t, err, "list boom")
	assert.True(t, errorx.IsOfType(err, errorx.ExternalError),
		"a failed release listing is an ExternalError, got %v", err)
}
