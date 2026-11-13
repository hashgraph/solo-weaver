// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
)

func fakeClient() (*kube.Client, error) { return &kube.Client{}, nil }

func hintsOf(err error) []string {
	hints, _ := errx.Hints(err)
	return hints
}

// stubStructural replaces the Helm manager, the health readers and the pod
// wait. It returns the mock and the list of pod waits.
func stubStructural(t *testing.T) (*helm.MockManager, *[]string) {
	t.Helper()
	hm := helm.NewMockManager(gomock.NewController(t))
	origHelm, origNE, origESO, origWait, origTimeout, origPoll :=
		newStructuralHelm, readNodeExporterHealth, readESOHealth, waitForPodsGone, structuralHealthTimeout, structuralPollInterval
	t.Cleanup(func() {
		newStructuralHelm, readNodeExporterHealth, readESOHealth, waitForPodsGone, structuralHealthTimeout, structuralPollInterval =
			origHelm, origNE, origESO, origWait, origTimeout, origPoll
	})
	newStructuralHelm = func() (helm.Manager, error) { return hm, nil }
	var waits []string
	waitForPodsGone = func(_ context.Context, _ *kube.Client, ns, prefix string) error {
		waits = append(waits, ns+"/"+prefix)
		return nil
	}
	structuralHealthTimeout, structuralPollInterval = 30*time.Millisecond, time.Millisecond
	return hm, &waits
}

func healthyNodeExporter(version string) nodeExporterHealth {
	return nodeExporterHealth{Chart: communityNodeExporterChart, Version: version, Deployed: true, Desired: 1, Ready: 1, Updated: 1, ServiceMonitors: 1}
}

func runAll(t *testing.T, builders []automa.Builder) []*automa.Report {
	t.Helper()
	return runSteps(t, builders)
}

func TestNodeExporterReplace_SwapsTheBitnamiChart(t *testing.T) {
	hm, waits := stubStructural(t)
	spec := chartSpec("node-exporter")
	readNodeExporterHealth = func(context.Context, helm.Manager, *kube.Client, *helmChartSpec) (nodeExporterHealth, error) {
		return healthyNodeExporter(spec.Version), nil
	}
	gomock.InOrder(
		hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf("node-exporter", "4.4.9", release.StatusDeployed), nil),
		hm.EXPECT().PullAndVerify(gomock.Any(), chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).Return("/charts/ne.tgz", nil),
		hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
		hm.EXPECT().InstallChart(gomock.Any(), spec.Release, "/charts/ne.tgz", "", spec.Namespace, gomock.Any()).
			DoAndReturn(func(_ context.Context, _, _, _, _ string, o helm.InstallChartOptions) (*release.Release, error) {
				assert.Equal(t, alloy.NodeExporterHelmValues(), o.ValueOpts)
				assert.True(t, o.Wait)
				return &release.Release{}, nil
			}),
	)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 4)
	for _, r := range reports {
		require.NoError(t, r.Error, r.Id)
		assert.Equal(t, automa.StatusSuccess, r.Status, r.Id)
	}
	assert.Equal(t, []string{spec.Namespace + "/" + alloy.NodeExporterAppName}, *waits, "the old pods are gone before the install")
}

func TestNodeExporterReplace_DoneSwapChangesNothing(t *testing.T) {
	hm, waits := stubStructural(t)
	spec := chartSpec("node-exporter")
	readNodeExporterHealth = func(context.Context, helm.Manager, *kube.Client, *helmChartSpec) (nodeExporterHealth, error) {
		return healthyNodeExporter(spec.Version), nil
	}
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf(communityNodeExporterChart, spec.Version, release.StatusDeployed), nil)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 4)
	for _, r := range reports[:3] {
		assert.Equal(t, automa.StatusSkipped, r.Status, r.Id)
	}
	assert.Equal(t, automa.StatusSuccess, reports[3].Status, "the health gate still passes")
	assert.Empty(t, *waits)
}

func TestNodeExporterReplace_FinishesASwapThatStoppedAfterTheUninstall(t *testing.T) {
	hm, waits := stubStructural(t)
	spec := chartSpec("node-exporter")
	readNodeExporterHealth = func(context.Context, helm.Manager, *kube.Client, *helmChartSpec) (nodeExporterHealth, error) {
		return healthyNodeExporter(spec.Version), nil
	}
	gomock.InOrder(
		hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(nil, notFound()),
		hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/charts/ne.tgz", nil),
		hm.EXPECT().InstallChart(gomock.Any(), spec.Release, "/charts/ne.tgz", "", spec.Namespace, gomock.Any()).Return(&release.Release{}, nil),
	)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 4)
	assert.Equal(t, automa.StatusSkipped, reports[1].Status, "nothing to uninstall")
	assert.Equal(t, automa.StatusSuccess, reports[3].Status)
	assert.Empty(t, *waits)
}

func TestNodeExporterReplace_RefusesAForeignChart(t *testing.T) {
	hm, _ := stubStructural(t)
	spec := chartSpec("node-exporter")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf("something-else", "1.0.0", release.StatusDeployed), nil)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Contains(t, reports[0].Error.Error(), `runs chart "something-else"`)
}

func TestNodeExporterReplace_RefusesAReleaseThatIsNotDeployed(t *testing.T) {
	hm, _ := stubStructural(t)
	spec := chartSpec("node-exporter")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf("node-exporter", "4.4.9", release.StatusFailed), nil)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Contains(t, reports[0].Error.Error(), "not deployed")
}

func TestNodeExporterReplace_FailedDownloadKeepsTheOldChart(t *testing.T) {
	hm, waits := stubStructural(t)
	spec := chartSpec("node-exporter")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf("node-exporter", "4.4.9", release.StatusDeployed), nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", assert.AnError)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Empty(t, *waits) // the mock fails the test on an UninstallChart call
}

func TestNodeExporterReplace_FailedInstallNamesTheRecovery(t *testing.T) {
	hm, _ := stubStructural(t)
	spec := chartSpec("node-exporter")
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf("node-exporter", "4.4.9", release.StatusDeployed), nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/charts/ne.tgz", nil)
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)
	hm.EXPECT().InstallChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, assert.AnError)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	require.Len(t, reports, 3)
	assert.Equal(t, automa.StatusFailed, reports[2].Status)
	assert.Contains(t, hintsOf(reports[2].Error), "Then run: sudo solo-provisioner alloy cluster install, which installs the prometheus-community node-exporter")
}

func TestNodeExporterReplace_UnhealthyAfterFails(t *testing.T) {
	hm, _ := stubStructural(t)
	spec := chartSpec("node-exporter")
	sick := healthyNodeExporter(spec.Version)
	sick.Ready = 0
	readNodeExporterHealth = func(context.Context, helm.Manager, *kube.Client, *helmChartSpec) (nodeExporterHealth, error) {
		return sick, nil
	}
	hm.EXPECT().GetRelease(spec.Release, spec.Namespace).Return(releaseOf(communityNodeExporterChart, spec.Version, release.StatusDeployed), nil)

	reports := runAll(t, NodeExporterReplaceSteps(fakeClient))

	last := reports[len(reports)-1]
	assert.Equal(t, automa.StatusFailed, last.Status)
	assert.Contains(t, last.Error.Error(), "DaemonSet: 1 desired, 1 updated, 0 ready")
}

func TestNodeExporterHealth_Problems(t *testing.T) {
	assert.Empty(t, healthyNodeExporter("4.59.0").problems("4.59.0"))
	h := nodeExporterHealth{Chart: "node-exporter", Version: "4.4.9", Deployed: true, Desired: 1, Ready: 1, Updated: 0}
	assert.ElementsMatch(t, []string{
		"the release runs node-exporter 4.4.9 (deployed=true), not prometheus-node-exporter 4.59.0",
		"DaemonSet: 1 desired, 0 updated, 1 ready",
		"0 ServiceMonitors match Alloy's selector, want 1",
	}, h.problems("4.59.0"))
}

// ---- External Secrets ----

const esoTo = "2.13.0"

func esoAt(version, namespace string, status release.Status) *release.Release {
	rel := esoRelease("external-secrets", namespace, esoChartName, status)
	rel.Chart.Metadata.Version = version
	return rel
}

func healthyESO(version string, ready map[string]bool) esoHealth {
	return esoHealth{
		Chart: esoChartName, Version: version, Deployed: true,
		Deployments: map[string]esoDeployment{
			"external-secrets":                 {Desired: 1, Available: 1, Image: "ghcr.io/external-secrets/external-secrets:v" + version},
			"external-secrets-webhook":         {Desired: 1, Available: 1, Image: "ghcr.io/external-secrets/external-secrets:v" + version},
			"external-secrets-cert-controller": {Desired: 1, Available: 1, Image: "ghcr.io/external-secrets/external-secrets:v" + version},
		},
		ExternalSecrets: ready,
	}
}

// esoReads returns the given healths in turn; the last one repeats.
func esoReads(healths ...esoHealth) {
	readESOHealth = func(context.Context, helm.Manager, *kube.Client, *helmChartSpec) (esoHealth, error) {
		h := healths[0]
		if len(healths) > 1 {
			healths = healths[1:]
		}
		return h, nil
	}
}

func TestExternalSecretsMigrate_UpgradesInPlace(t *testing.T) {
	for _, ns := range []string{"external-secrets", "secrets"} {
		t.Run(ns, func(t *testing.T) {
			hm, _ := stubStructural(t)
			target, err := resolveCatalogChartVersion(esoChartName, esoTo)
			require.NoError(t, err)
			ready := map[string]bool{"app/db": true, "app/broken": false}
			esoReads(healthyESO("0.20.2", ready), healthyESO(esoTo, ready))
			gomock.InOrder(
				hm.EXPECT().ListAll().Return([]*release.Release{esoAt("0.20.2", ns, release.StatusDeployed)}, nil),
				hm.EXPECT().AddRepo(target.RepoAlias, target.Repo, gomock.Any()).Return(nil, nil),
				hm.EXPECT().PullAndVerify(gomock.Any(), chartDownloadsDir(), target.Chart, esoTo, target.Algorithm, target.Checksum).Return("/charts/eso.tgz", nil),
				hm.EXPECT().UpgradeChart(gomock.Any(), "external-secrets", "/charts/eso.tgz", "", ns, gomock.Any()).
					DoAndReturn(func(_ context.Context, _, _, _, _ string, o helm.UpgradeChartOptions) (*release.Release, error) {
						assert.Equal(t, esoHelmValues(), o.ValueOpts)
						assert.False(t, o.ReuseValues, "our values replace the 0.x ones")
						assert.False(t, o.Atomic)
						assert.True(t, o.Wait)
						return &release.Release{}, nil
					}),
			)

			reports := runAll(t, ExternalSecretsMigrateSteps(esoTo, fakeClient))

			require.Len(t, reports, 3)
			for _, r := range reports {
				require.NoError(t, r.Error, r.Id)
				assert.Equal(t, automa.StatusSuccess, r.Status, r.Id)
			}
		})
	}
}

func TestExternalSecretsMigrate_DoneMigrationChangesNothing(t *testing.T) {
	hm, _ := stubStructural(t)
	esoReads(healthyESO(esoTo, nil))
	hm.EXPECT().ListAll().Return([]*release.Release{esoAt(esoTo, "external-secrets", release.StatusDeployed)}, nil)

	reports := runAll(t, ExternalSecretsMigrateSteps(esoTo, fakeClient))

	require.Len(t, reports, 3)
	assert.Equal(t, automa.StatusSkipped, reports[0].Status)
	assert.Equal(t, automa.StatusSkipped, reports[1].Status)
	assert.Equal(t, automa.StatusSuccess, reports[2].Status)
}

func TestExternalSecretsMigrate_RefusesWithoutExactlyOneDeployedRelease(t *testing.T) {
	for name, tc := range map[string]struct {
		releases []*release.Release
		want     string
	}{
		"none":         {nil, "found 0 External Secrets releases"},
		"two":          {[]*release.Release{esoAt("0.20.2", "a", release.StatusDeployed), esoAt("0.20.2", "b", release.StatusDeployed)}, "found 2"},
		"not deployed": {[]*release.Release{esoAt("0.20.2", "external-secrets", release.StatusFailed)}, "not deployed"},
	} {
		t.Run(name, func(t *testing.T) {
			hm, _ := stubStructural(t)
			hm.EXPECT().ListAll().Return(tc.releases, nil)

			reports := runAll(t, ExternalSecretsMigrateSteps(esoTo, fakeClient))

			require.Len(t, reports, 1)
			assert.Equal(t, automa.StatusFailed, reports[0].Status)
			assert.Contains(t, reports[0].Error.Error(), tc.want)
		})
	}
}

func TestExternalSecretsMigrate_FailedUpgradeHintsTheRollback(t *testing.T) {
	hm, _ := stubStructural(t)
	esoReads(healthyESO("0.20.2", nil))
	hm.EXPECT().ListAll().Return([]*release.Release{esoAt("0.20.2", "external-secrets", release.StatusDeployed)}, nil)
	hm.EXPECT().AddRepo(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/charts/eso.tgz", nil)
	hm.EXPECT().UpgradeChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, assert.AnError)

	reports := runAll(t, ExternalSecretsMigrateSteps(esoTo, fakeClient))

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusFailed, reports[1].Status)
	assert.Contains(t, hintsOf(reports[1].Error), "To go back: helm rollback external-secrets -n external-secrets")
}

func TestExternalSecretsMigrate_AnExternalSecretThatStopsBeingReadyFails(t *testing.T) {
	hm, _ := stubStructural(t)
	esoReads(healthyESO("0.20.2", map[string]bool{"app/db": true}), healthyESO(esoTo, map[string]bool{"app/db": false}))
	hm.EXPECT().ListAll().Return([]*release.Release{esoAt("0.20.2", "external-secrets", release.StatusDeployed)}, nil)
	hm.EXPECT().AddRepo(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("/charts/eso.tgz", nil)
	hm.EXPECT().UpgradeChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&release.Release{}, nil)

	reports := runAll(t, ExternalSecretsMigrateSteps(esoTo, fakeClient))

	last := reports[len(reports)-1]
	assert.Equal(t, automa.StatusFailed, last.Status)
	assert.Contains(t, last.Error.Error(), "ExternalSecret app/db was Ready before the upgrade and is not now")
}

func TestESOHealth_Problems(t *testing.T) {
	assert.Empty(t, healthyESO(esoTo, map[string]bool{"a/b": true}).problems(esoTo, []string{"a/b"}))

	h := healthyESO("0.20.2", nil)
	h.Deployments["external-secrets-webhook"] = esoDeployment{Desired: 1, Available: 0, Image: "ghcr.io/external-secrets/external-secrets:v2.13.0"}
	assert.ElementsMatch(t, []string{
		"the release runs external-secrets 0.20.2 (deployed=true), not external-secrets 2.13.0",
		"external-secrets runs ghcr.io/external-secrets/external-secrets:v0.20.2, not v2.13.0",
		"external-secrets-cert-controller runs ghcr.io/external-secrets/external-secrets:v0.20.2, not v2.13.0",
		"external-secrets-webhook: 0 of 1 available",
	}, h.problems(esoTo, nil))

	assert.Contains(t, esoHealth{}.problems(esoTo, nil), "no controller Deployments")
	assert.Equal(t, []string{"a/x", "b/y"}, esoHealth{ExternalSecrets: map[string]bool{"b/y": true, "a/x": true, "c/z": false}}.readyExternalSecrets())
}
