// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
)

// stubChartHop replaces Helm, the API server version and each recipe's extra
// checks. It returns the mock and the charts whose extra checks ran.
func stubChartHop(t *testing.T, serverVersion string) (*helm.MockManager, *[]string) {
	t.Helper()
	hm := helm.NewMockManager(gomock.NewController(t))
	origHelm, origVer, origPoll, origTimeout := newChartHopHelm, chartHopServerVer, chartHopPollInterval, chartHopTimeout
	origRecipes := make(map[string]chartRecipe, len(chartRecipes))
	for k, v := range chartRecipes {
		origRecipes[k] = v
	}
	t.Cleanup(func() {
		newChartHopHelm, chartHopServerVer, chartHopPollInterval, chartHopTimeout = origHelm, origVer, origPoll, origTimeout
		chartRecipes = origRecipes
	})
	newChartHopHelm = func() (helm.Manager, error) { return hm, nil }
	chartHopServerVer = func() (string, error) { return serverVersion, nil }
	chartHopPollInterval, chartHopTimeout = time.Millisecond, 20*time.Millisecond

	var extras []string
	stubbed := map[string]chartRecipe{}
	for name, r := range origRecipes {
		if r.extra != nil {
			name := name
			r.extra = func(context.Context, *kube.Client, *helmChartSpec) ([]string, error) {
				extras = append(extras, name)
				return nil, nil
			}
		}
		stubbed[name] = r
	}
	chartRecipes = stubbed
	return hm, &extras
}

// expectChartUpgrade expects b's release to be read at b.From, upgraded with
// opts checked by check, and then read at b.To.
func expectChartUpgrade(t *testing.T, hm *helm.MockManager, b ChartBump, check func(helm.UpgradeChartOptions)) *gomock.Call {
	t.Helper()
	spec, err := resolveCatalogChartVersion(b.Chart, b.To)
	require.NoError(t, err)
	calls := []*gomock.Call{hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(relAt(spec, b.Chart, b.From, release.StatusDeployed), nil)}
	if spec.Repo != "" {
		calls = append(calls, hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil))
	}
	calls = append(calls,
		hm.EXPECT().PullAndVerify(gomock.Any(), chartDownloadsDir(), spec.Chart, b.To, spec.Algorithm, spec.Checksum).Return("/charts/"+b.Chart+".tgz", nil),
		hm.EXPECT().UpgradeChart(gomock.Any(), spec.Release, "/charts/"+b.Chart+".tgz", "", spec.Namespace, gomock.Any()).
			DoAndReturn(func(_ context.Context, _, _, _, _ string, o helm.UpgradeChartOptions) (*release.Release, error) {
				require.NotNil(t, o.ValueOpts, "a nil value set makes Helm reuse the release's values")
				assert.True(t, o.Wait)
				if check != nil {
					check(o)
				}
				return &release.Release{}, nil
			}),
		hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(relAt(spec, b.Chart, b.To, release.StatusDeployed), nil),
	)
	gomock.InOrder(calls...)
	return calls[len(calls)-1]
}

// relAt is a release as Helm returns it, with its name and namespace set.
func relAt(spec *helmChartSpec, chart, version string, status release.Status) *release.Release {
	rel := releaseOf(chart, version, status)
	rel.Name, rel.Namespace = spec.Release, spec.Namespace
	return rel
}

func chartHopRun(t *testing.T, bumps ...ChartBump) []*automa.Report {
	t.Helper()
	return runSteps(t, ChartHopSteps(bumps, fakeClient))
}

var refresh = []ChartBump{
	{Chart: "alloy", From: "1.4.0", To: "1.13.1"},
	{Chart: "metrics-server", From: "3.13.0", To: "3.14.0"},
	{Chart: "prometheus-operator-crds", From: "24.0.1", To: "32.0.1"},
}

func TestChartHop_RefreshUpgradesEachChartCRDsFirst(t *testing.T) {
	hm, extras := stubChartHop(t, "v1.36.5")
	alloyJSON, err := alloy.ChartJSONValues("1.13.1")
	require.NoError(t, err)
	crds := expectChartUpgrade(t, hm, refresh[2], func(o helm.UpgradeChartOptions) {
		assert.False(t, o.ReuseValues)
		assert.Equal(t, &values.Options{}, o.ValueOpts)
	})
	a := expectChartUpgrade(t, hm, refresh[0], func(o helm.UpgradeChartOptions) {
		assert.True(t, o.ReuseValues, "Alloy keeps the values it was installed with")
		assert.Equal(t, alloyJSON, o.ValueOpts.JSONValues)
		assert.Empty(t, o.ValueOpts.Values)
	})
	ms := expectChartUpgrade(t, hm, refresh[1], func(o helm.UpgradeChartOptions) {
		assert.False(t, o.ReuseValues)
		assert.Equal(t, defaultMetricsServerValues(), o.ValueOpts)
	})
	gomock.InOrder(crds, a, ms)

	reports := chartHopRun(t, refresh...)

	require.Len(t, reports, 4)
	for _, r := range reports {
		require.NoError(t, r.Error, r.Id)
		assert.Equal(t, automa.StatusSuccess, r.Status, r.Id)
	}
	assert.Equal(t, []string{"check-chart-hop", "upgrade-chart-prometheus-operator-crds", "upgrade-chart-alloy", "upgrade-chart-metrics-server"},
		[]string{reports[0].Id, reports[1].Id, reports[2].Id, reports[3].Id})
	assert.Equal(t, []string{"prometheus-operator-crds", "metrics-server"}, *extras)
}

func TestChartHop_CoupledMetalLB(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.35.10")
	expectChartUpgrade(t, hm, ChartBump{Chart: "metallb", From: "0.15.2", To: "0.16.1"}, func(o helm.UpgradeChartOptions) {
		assert.False(t, o.ReuseValues)
		assert.Equal(t, metalLBHelmValues(), o.ValueOpts.Values)
	})

	reports := chartHopRun(t, ChartBump{Chart: "metallb", From: "0.15.2", To: "0.16.1"})

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusSuccess, reports[1].Status)
}

func TestChartHop_ChartAtItsTargetIsSkipped(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.35.10")
	spec := chartSpec("metallb")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(relAt(spec, "metallb", "0.16.1", release.StatusDeployed), nil).Times(2)

	reports := chartHopRun(t, ChartBump{Chart: "metallb", From: "0.15.2", To: "0.16.1"})

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusSkipped, reports[1].Status) // the mock fails the test on any upgrade call
}

func TestChartHop_ChartElsewhereIsRefused(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.35.10")
	spec := chartSpec("metallb")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(relAt(spec, "metallb", "0.14.9", release.StatusDeployed), nil)

	reports := chartHopRun(t, ChartBump{Chart: "metallb", From: "0.15.2", To: "0.16.1"})

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusFailed, reports[1].Status)
	assert.Contains(t, reports[1].Error.Error(), "metallb runs 0.14.9, not the hop's start 0.15.2")
}

func TestChartHop_MissingReleaseIsRefused(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.35.10")
	spec := chartSpec("metallb")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(nil, notFound())

	reports := chartHopRun(t, ChartBump{Chart: "metallb", From: "0.15.2", To: "0.16.1"})

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusFailed, reports[1].Status)
	assert.Contains(t, reports[1].Error.Error(), "has no release to upgrade")
}

func TestChartHop_MetricsServerMustSuitTheRunningKubernetes(t *testing.T) {
	stubChartHop(t, "v1.33.13") // 3.14.0 needs 1.34 or later

	reports := chartHopRun(t, refresh...)

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Contains(t, reports[0].Error.Error(), "metrics-server 3.14.0 is not the chart for Kubernetes v1.33.13")
}

func TestChartHop_UnknownChartIsRefused(t *testing.T) {
	stubChartHop(t, "v1.36.5")

	reports := chartHopRun(t, ChartBump{Chart: "cilium", From: "1.18.1", To: "1.18.16"})

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Contains(t, reports[0].Error.Error(), "cannot upgrade chart cilium")
}

func TestChartHop_FailedUpgradeStopsTheHop(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.36.5")
	spec, err := resolveCatalogChartVersion("prometheus-operator-crds", "32.0.1")
	require.NoError(t, err)
	gomock.InOrder(
		hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(relAt(spec, "prometheus-operator-crds", "24.0.1", release.StatusDeployed), nil),
		hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/c.tgz", nil),
		hm.EXPECT().UpgradeChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, assert.AnError),
	)

	reports := chartHopRun(t, refresh...)

	require.Len(t, reports, 2, "alloy and metrics-server are not touched")
	assert.Equal(t, automa.StatusFailed, reports[1].Status)
	assert.Contains(t, hintsOf(reports[1].Error), "To go back: helm rollback prometheus-operator-crds -n "+spec.Namespace)
}

func TestChartHop_UnhealthyAfterFails(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.36.5")
	r := chartRecipes["metrics-server"]
	r.extra = func(context.Context, *kube.Client, *helmChartSpec) ([]string, error) {
		return []string{"APIService v1beta1.metrics.k8s.io is not Available"}, nil
	}
	chartRecipes["metrics-server"] = r
	spec := chartSpec("metrics-server")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(relAt(spec, "metrics-server", "3.14.0", release.StatusDeployed), nil).AnyTimes()

	reports := chartHopRun(t, ChartBump{Chart: "metrics-server", From: "3.13.0", To: "3.14.0"})

	last := reports[len(reports)-1]
	assert.Equal(t, automa.StatusFailed, last.Status)
	assert.Contains(t, last.Error.Error(), "metrics-server 3.14.0 is not healthy after")
	assert.Contains(t, last.Error.Error(), "APIService v1beta1.metrics.k8s.io is not Available")
}

func TestChartHop_ExternalSecretsIsFoundInItsNamespace(t *testing.T) {
	hm, _ := stubChartHop(t, "v1.36.5")
	hm.EXPECT().ListAll().Return([]*release.Release{esoAt("0.20.2", "secrets", release.StatusDeployed)}, nil)
	hm.EXPECT().AddRepo(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/eso.tgz", nil)
	hm.EXPECT().UpgradeChart(gomock.Any(), "external-secrets", "/eso.tgz", "", "secrets", gomock.Any()).Return(&release.Release{}, nil)
	hm.EXPECT().GetRelease("external-secrets", "secrets").Return(releaseOf(esoChartName, "2.13.0", release.StatusDeployed), nil)

	reports := chartHopRun(t, ChartBump{Chart: "external-secrets", From: "0.20.2", To: "2.13.0"})

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusSuccess, reports[1].Status)
}

func TestOrderBumps(t *testing.T) {
	got := orderBumps([]ChartBump{{Chart: "metrics-server"}, {Chart: "prometheus-operator-crds"}, {Chart: "alloy"}})
	assert.Equal(t, []ChartBump{{Chart: "prometheus-operator-crds"}, {Chart: "metrics-server"}, {Chart: "alloy"}}, got)
}

func TestChartHasRecipe(t *testing.T) {
	for _, c := range []string{"alloy", "external-secrets", "metallb", "metrics-server", "node-exporter", "prometheus-operator-crds"} {
		assert.True(t, ChartHasRecipe(c), c)
	}
	assert.False(t, ChartHasRecipe("cilium"))
}

func withCondition(u *unstructured.Unstructured, kind, status string) *unstructured.Unstructured {
	_ = unstructured.SetNestedSlice(u.Object, []any{map[string]any{"type": kind, "status": status}}, "status", "conditions")
	return u
}

func object(apiVersion, kind, name string, annotations map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetName(name)
	u.SetAnnotations(annotations)
	return u
}

func extrasCluster(objs ...runtime.Object) *kube.Client {
	return &kube.Client{Dyn: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		apiServicesGVR: "APIServiceList",
		crdsGVR:        "CustomResourceDefinitionList",
	}, objs...)}
}

func TestMetricsAPIProblems(t *testing.T) {
	ctx := context.Background()
	up := extrasCluster(withCondition(object("apiregistration.k8s.io/v1", "APIService", metricsAPIService, nil), "Available", "True"))
	p, err := metricsAPIProblems(ctx, up, nil)
	require.NoError(t, err)
	assert.Empty(t, p)

	down := extrasCluster(withCondition(object("apiregistration.k8s.io/v1", "APIService", metricsAPIService, nil), "Available", "False"))
	p, err = metricsAPIProblems(ctx, down, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"APIService v1beta1.metrics.k8s.io is not Available"}, p)

	_, err = metricsAPIProblems(ctx, extrasCluster(), nil)
	require.Error(t, err)
}

func TestCRDsEstablishedProblems(t *testing.T) {
	ctx := context.Background()
	spec := &helmChartSpec{Release: "prometheus-operator-crds", Namespace: "grafana-alloy"}
	owner := map[string]string{"meta.helm.sh/release-name": spec.Release, "meta.helm.sh/release-namespace": spec.Namespace}
	crd := func(name, status string, a map[string]string) runtime.Object {
		return withCondition(object("apiextensions.k8s.io/v1", "CustomResourceDefinition", name, a), "Established", status)
	}

	p, err := crdsEstablishedProblems(ctx, extrasCluster(crd("servicemonitors.monitoring.coreos.com", "True", owner), crd("other.example.com", "False", nil)), spec)
	require.NoError(t, err)
	assert.Empty(t, p, "CRDs of other releases do not count")

	p, err = crdsEstablishedProblems(ctx, extrasCluster(crd("podmonitors.monitoring.coreos.com", "False", owner)), spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"CRD podmonitors.monitoring.coreos.com is not Established"}, p)

	p, err = crdsEstablishedProblems(ctx, extrasCluster(), spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"no CRDs belong to release grafana-alloy/prometheus-operator-crds"}, p)
}
