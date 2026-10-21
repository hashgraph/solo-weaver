// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Every chart version the catalog lists is verified, whether it is the default
// or was asked for. Teleport 18.6.4 is what earlier releases installed.
func TestTeleportKubeAgentChart_CatalogVersionsAreVerified(t *testing.T) {
	defaults, err := resolveCatalogChart(teleportClusterAgent)
	require.NoError(t, err)

	for _, tc := range []struct{ requested, version, checksum string }{
		{"", defaults.Version, defaults.Checksum},
		{defaults.Version, defaults.Version, defaults.Checksum},
		{"18.6.4", "18.6.4", "f657f1f216ebfe6bd074d5c3509b6617205ae956d3e73b895e20d71b26002201"},
		{"18.11.3", "18.11.3", "60e7dd096337c6b817076f2d0a7695e7dc50cd8262306a9dc4537f7449865405"},
	} {
		t.Run("requested "+tc.requested, func(t *testing.T) {
			spec, verified, err := teleportKubeAgentChart(tc.requested)
			require.NoError(t, err)
			assert.True(t, verified)
			assert.Equal(t, tc.version, spec.Version)
			assert.Equal(t, "sha256", spec.Algorithm)
			assert.Equal(t, tc.checksum, spec.Checksum)
			assert.Equal(t, defaults.Chart, spec.Chart)
		})
	}
}

// A version the catalog does not list keeps the legacy behaviour: it installs
// from the repository with no checksum. The match is exact.
func TestTeleportKubeAgentChart_UnlistedVersionIsUnverified(t *testing.T) {
	defaults, err := resolveCatalogChart(teleportClusterAgent)
	require.NoError(t, err)

	for _, requested := range []string{"16.0.0", "v18.6.4", "~18.6"} {
		t.Run(requested, func(t *testing.T) {
			spec, verified, err := teleportKubeAgentChart(requested)
			require.NoError(t, err)
			assert.False(t, verified)
			assert.Equal(t, requested, spec.Version)
			assert.Empty(t, spec.Algorithm)
			assert.Empty(t, spec.Checksum)
			assert.Equal(t, defaults.Chart, spec.Chart)
			assert.Equal(t, defaults.Release, spec.Release)
			assert.Equal(t, defaults.Namespace, spec.Namespace)
		})
	}
}

// A catalog that cannot be read is an error, never a reason to install an
// unverified chart.
func TestTeleportKubeAgentChart_CatalogErrorFailsClosed(t *testing.T) {
	orig := loadCatalog
	t.Cleanup(func() { loadCatalog = orig })
	loadCatalog = func() (*software.InfrastructureCatalog, error) { return nil, errors.New("catalog unreadable") }

	for _, requested := range []string{"", "18.6.4", "16.0.0"} {
		spec, verified, err := teleportKubeAgentChart(requested)
		require.Error(t, err, "requested %q", requested)
		assert.False(t, verified)
		assert.Nil(t, spec)
	}
}

func withTeleportVersion(t *testing.T, version string) {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { _ = config.Set(&orig) })
	c := orig
	c.Teleport.Version = version
	c.Teleport.ValuesFile = "/tmp/teleport-values.yaml"
	require.NoError(t, config.Set(&c))
}

func runInstallTeleportKubeAgent(t *testing.T, hm helm.Manager) (map[string]string, error) {
	t.Helper()
	step, err := installTeleportKubeAgent(managerFor(hm)).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	return report.Metadata, report.Error
}

func TestInstallTeleportKubeAgent_PullsAndVerifiesCatalogVersions(t *testing.T) {
	for _, requested := range []string{"", "18.6.4", "18.11.3"} {
		t.Run("requested "+requested, func(t *testing.T) {
			withTeleportVersion(t, requested)
			spec, verified, err := teleportKubeAgentChart(requested)
			require.NoError(t, err)
			require.True(t, verified)
			localChart := "/tmp/charts/teleport-kube-agent-" + spec.Version + ".tgz"

			// gomock fails on any call not listed here, including a second pull.
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, teleportClusterAgent, nil, notFound())
			hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
			hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, "sha256", spec.Checksum).Return(localChart, nil)
			hm.EXPECT().InstallChart(gomock.Any(), spec.Release, localChart, "", spec.Namespace, gomock.Any()).Return(nil, nil)

			meta, err := runInstallTeleportKubeAgent(t, hm)
			require.NoError(t, err)
			assert.Equal(t, spec.Version, meta["chartVersion"])
			assert.Equal(t, "true", meta["chartVerified"])
			assert.Equal(t, "true", meta[InstalledByThisStep])
		})
	}
}

func TestInstallTeleportKubeAgent_PullFailureStopsTheInstall(t *testing.T) {
	withTeleportVersion(t, "18.6.4")
	spec, _, err := teleportKubeAgentChart("18.6.4")
	require.NoError(t, err)

	hm := helm.NewMockManager(gomock.NewController(t))
	expectRelease(hm, teleportClusterAgent, nil, notFound())
	hm.EXPECT().AddRepo(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, "18.6.4", "sha256", spec.Checksum).
		Return("", errors.New("checksum mismatch"))

	_, err = runInstallTeleportKubeAgent(t, hm)
	require.Error(t, err)
}

func TestInstallTeleportKubeAgent_UnlistedVersionInstallsUnverified(t *testing.T) {
	withTeleportVersion(t, "16.0.0")
	spec := chartSpec(teleportClusterAgent)

	hm := helm.NewMockManager(gomock.NewController(t))
	expectRelease(hm, teleportClusterAgent, nil, notFound())
	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().InstallChart(gomock.Any(), spec.Release, spec.Chart, "16.0.0", spec.Namespace, gomock.Any()).Return(nil, nil)

	meta, err := runInstallTeleportKubeAgent(t, hm)
	require.NoError(t, err)
	assert.Equal(t, "16.0.0", meta["chartVersion"])
	assert.Equal(t, "false", meta["chartVerified"])
}

// A deployed release is left alone whatever chart version it runs, even when
// --version asks for another one.
func TestInstallTeleportKubeAgent_DeployedReleaseIsSkipped(t *testing.T) {
	for _, requested := range []string{"", "18.11.3"} {
		t.Run("requested "+requested, func(t *testing.T) {
			withTeleportVersion(t, requested)
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, teleportClusterAgent, releaseOf("teleport-kube-agent", "18.6.4", release.StatusDeployed), nil)

			meta, err := runInstallTeleportKubeAgent(t, hm)
			require.NoError(t, err)
			assert.Equal(t, "true", meta[AlreadyInstalled])
			assert.Empty(t, meta[InstalledByThisStep])
		})
	}
}

// A release Helm cannot install over is refused before the chart is fetched.
func TestInstallTeleportKubeAgent_RefusesAReleaseThatIsNotDeployed(t *testing.T) {
	withTeleportVersion(t, "")
	for _, status := range notDeployedStates {
		t.Run(string(status), func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, teleportClusterAgent, releaseOf("teleport-kube-agent", "18.6.4", status), nil)

			_, err := runInstallTeleportKubeAgent(t, hm)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not deployed")
		})
	}

	t.Run("lookup error", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, teleportClusterAgent, nil, errors.New("connection refused"))

		_, err := runInstallTeleportKubeAgent(t, hm)
		require.Error(t, err)
	})
}
