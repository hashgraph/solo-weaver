// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"errors"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

func releaseOf(chartName, version string, status release.Status) *release.Release {
	return &release.Release{
		Info:  &release.Info{Status: status},
		Chart: &chart.Chart{Metadata: &chart.Metadata{Name: chartName, Version: version}},
	}
}

func notFound() error { return helm.ErrNotFound.New("release not found") }

// expectRelease makes GetRelease for the catalog release of name return rel, err.
func expectRelease(hm *helm.MockManager, name string, rel *release.Release, err error) {
	spec := chartSpec(name)
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(rel, err)
}

var notDeployedStates = []release.Status{
	release.StatusUnknown,
	release.StatusUninstalled,
	release.StatusSuperseded,
	release.StatusFailed,
	release.StatusUninstalling,
	release.StatusPendingInstall,
	release.StatusPendingUpgrade,
	release.StatusPendingRollback,
}

func TestDeployedRelease(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "alloy", nil, notFound())
		rel, err := deployedRelease(hm, chartSpec("alloy"))
		require.NoError(t, err)
		assert.Nil(t, rel)
	})
	t.Run("deployed", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		want := releaseOf("alloy", "1.4.0", release.StatusDeployed)
		expectRelease(hm, "alloy", want, nil)
		rel, err := deployedRelease(hm, chartSpec("alloy"))
		require.NoError(t, err)
		assert.Same(t, want, rel)
	})
	t.Run("lookup error", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		expectRelease(hm, "alloy", nil, errors.New("connection refused"))
		_, err := deployedRelease(hm, chartSpec("alloy"))
		require.Error(t, err)
		assertReason(t, err, reasons.K8sAPIFailed)
	})
	for _, status := range notDeployedStates {
		t.Run(string(status), func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, "alloy", releaseOf("alloy", "1.4.0", status), nil)
			_, err := deployedRelease(hm, chartSpec("alloy"))
			require.Error(t, err)
			assertReason(t, err, reasons.PreconditionNotMet)
		})
	}
	for name, rel := range map[string]*release.Release{
		"nil release": nil,
		"nil info":    {Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "alloy", Version: "1.4.0"}}},
	} {
		t.Run(name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, "alloy", rel, nil)
			_, err := deployedRelease(hm, chartSpec("alloy"))
			require.Error(t, err)
			assertReason(t, err, reasons.PreconditionNotMet)
		})
	}
}

func TestResolveDeployedChart_Alloy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rel      *release.Release
		version  string
		existing bool
	}{
		{"absent gets the default", nil, "1.4.0", false},
		{"deployed 1.4.0 keeps 1.4.0", releaseOf("alloy", "1.4.0", release.StatusDeployed), "1.4.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			var err error
			if tc.rel == nil {
				err = notFound()
			}
			expectRelease(hm, "alloy", tc.rel, err)

			spec, existing, err := resolveDeployedChart(hm, "alloy")
			require.NoError(t, err)
			assert.Equal(t, tc.version, spec.Version)
			assert.Equal(t, tc.existing, existing)
			want, err := resolveCatalogChartVersion("alloy", tc.version)
			require.NoError(t, err)
			assert.Equal(t, want.Checksum, spec.Checksum)
		})
	}
}

func TestResolveDeployedChart_AlloyRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		rel    *release.Release
		err    error
		reason errx.Reason
	}{
		"version not in catalog":   {rel: releaseOf("alloy", "1.9.0", release.StatusDeployed), reason: reasons.PreconditionNotMet},
		"newer than the default":   {rel: releaseOf("alloy", "1.14.0", release.StatusDeployed), reason: reasons.PreconditionNotMet},
		"empty version":            {rel: releaseOf("alloy", "", release.StatusDeployed), reason: reasons.PreconditionNotMet},
		"another chart":            {rel: releaseOf("grafana-agent", "1.4.0", release.StatusDeployed), reason: reasons.PreconditionNotMet},
		"no chart":                 {rel: &release.Release{Info: &release.Info{Status: release.StatusDeployed}}, reason: reasons.PreconditionNotMet},
		"no chart metadata":        {rel: &release.Release{Info: &release.Info{Status: release.StatusDeployed}, Chart: &chart.Chart{}}, reason: reasons.PreconditionNotMet},
		"failed upgrade":           {rel: releaseOf("alloy", "1.13.1", release.StatusFailed), reason: reasons.PreconditionNotMet},
		"pending upgrade":          {rel: releaseOf("alloy", "1.13.1", release.StatusPendingUpgrade), reason: reasons.PreconditionNotMet},
		"uninstalled with history": {rel: releaseOf("alloy", "1.4.0", release.StatusUninstalled), reason: reasons.PreconditionNotMet},
		"lookup error":             {err: errors.New("connection refused"), reason: reasons.K8sAPIFailed},
	} {
		t.Run(name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			expectRelease(hm, "alloy", tc.rel, tc.err)

			spec, existing, err := resolveDeployedChart(hm, "alloy")
			require.Error(t, err)
			assert.Nil(t, spec)
			assert.False(t, existing)
			assertReason(t, err, tc.reason)
		})
	}
}
