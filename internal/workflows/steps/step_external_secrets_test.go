// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func Test_SetupExternalSecrets_VersionResolution(t *testing.T) {
	t.Run("default version", func(t *testing.T) {
		def, err := resolveCatalogChart("external-secrets")
		require.NoError(t, err)

		spec, err := resolveCatalogChartVersion("external-secrets", "")
		require.NoError(t, err)
		assert.Equal(t, def.Version, spec.Version)
	})

	t.Run("declared version accepted", func(t *testing.T) {
		def, err := resolveCatalogChart("external-secrets")
		require.NoError(t, err)

		spec, err := resolveCatalogChartVersion("external-secrets", def.Version)
		require.NoError(t, err)
		assert.Equal(t, def.Version, spec.Version)
		assert.NotEmpty(t, spec.Checksum)
	})

	t.Run("undeclared version rejected", func(t *testing.T) {
		_, err := SetupExternalSecrets(ESOInstallOptions{Version: "0.0.0-nonexistent"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "0.0.0-nonexistent")
	})
}
