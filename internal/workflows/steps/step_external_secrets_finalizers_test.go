// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

type fakeCRD struct{ terminating bool }

// fakeESOCluster is an esoClusterReader over fixed CRDs, objects and Secret
// metadata. A terminating CRD vanishes after goneAfter CRDState calls for it,
// when set.
type fakeESOCluster struct {
	crds      map[string]fakeCRD
	crdErr    error
	goneAfter int
	lookups   []string
	calls     map[string]int

	objects map[schema.GroupKind][]unstructured.Unstructured
	listErr error

	// secrets maps namespace/name to the Secret's metadata.
	secrets   map[string]map[string]interface{}
	secretErr error
}

func (f *fakeESOCluster) CRDState(_ context.Context, name string) (bool, bool, error) {
	f.lookups = append(f.lookups, name)
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[name]++
	if f.crdErr != nil {
		return false, false, f.crdErr
	}
	crd, ok := f.crds[name]
	if ok && crd.terminating && f.goneAfter > 0 && f.calls[name] > f.goneAfter {
		return false, false, nil
	}
	return ok, ok && crd.terminating, nil
}

func (f *fakeESOCluster) ListResources(_ context.Context, gk schema.GroupKind, _ string) ([]unstructured.Unstructured, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.objects[gk], nil
}

func (f *fakeESOCluster) GetResourceNestedMap(_ context.Context, apiVersion, kind, namespace, name string, fields ...string) (map[string]interface{}, bool, error) {
	if f.secretErr != nil {
		return nil, false, f.secretErr
	}
	m, ok := f.secrets[namespace+"/"+name]
	return m, ok, nil
}

func esoObj(kd esoFinalizedKind, namespace, name string, deleting bool) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion(kd.group + "/v1")
	u.SetKind(kd.kind)
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetFinalizers([]string{"externalsecrets.external-secrets.io/externalsecret-cleanup"})
	if deleting {
		now := metav1.Now()
		u.SetDeletionTimestamp(&now)
	}
	return u
}

// allESOCRDs is every ESO CRD, present, and terminating when asked.
func allESOCRDs(terminating bool) map[string]fakeCRD {
	m := map[string]fakeCRD{}
	for _, name := range esoCRDs {
		m[name] = fakeCRD{terminating: terminating}
	}
	return m
}

func ownedSecretMeta() map[string]interface{} {
	return map[string]interface{}{
		"ownerReferences": []interface{}{
			map[string]interface{}{"apiVersion": "external-secrets.io/v1", "kind": "ExternalSecret", "name": "x", "uid": "es-uid"},
		},
	}
}

func requireReason(t *testing.T, err error, want errx.Reason) []string {
	t.Helper()
	require.Error(t, err)
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "reason attached")
	assert.Equal(t, want, reason)
	hints, _ := errx.Hints(err)
	return hints
}

func deployedESO(spec *helmChartSpec) []*release.Release {
	return []*release.Release{esoRelease(spec.Release, spec.Namespace, esoChartName, release.StatusDeployed)}
}

// deployedESOWithCRDs is deployedESO with a manifest that declares crds.
func deployedESOWithCRDs(spec *helmChartSpec, crds ...string) []*release.Release {
	rels := deployedESO(spec)
	manifest := "---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: external-secrets\n"
	for _, name := range crds {
		manifest += "---\napiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: " + name + "\n"
	}
	rels[0].Manifest = manifest
	return rels
}

// recoveringESO is deployedESOWithCRDs labelled as a recovery that has not
// finished.
func recoveringESO(spec *helmChartSpec, crds ...string) []*release.Release {
	rels := deployedESOWithCRDs(spec, crds...)
	rels[0].Labels = map[string]string{esoRecoveryLabel: "draining"}
	return rels
}

// Each finalized kind blocks the uninstall before Helm is called: the mock has
// no UninstallChart expectation, so a call would fail the test.
func Test_uninstallESOChart_RefusesWhileFinalizedObjectsExist(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	for _, kd := range esoFinalizedKinds {
		t.Run(kd.kind, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil)
			k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
				kd.groupKind(): {esoObj(kd, "team-a", "obj", false)},
			}}

			rel, err := uninstallESOChart(context.Background(), hm, k, spec)
			assert.Nil(t, rel)
			hints := requireReason(t, err, reasons.PreconditionNotMet)
			assert.True(t, errorx.IsOfType(err, errorx.IllegalState))
			assert.Contains(t, err.Error(), "team-a/obj ("+kd.kind+")")
			assert.Contains(t, hints, "See what still uses them: kubectl get "+kd.kubectlName()+" -A")
		})
	}
}

func Test_uninstallESOChart_ExternalSecretHintsNameTheCLI(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil)
	k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
		externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", false)},
	}}

	_, err = uninstallESOChart(context.Background(), hm, k, spec)
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, hints, "  sudo solo-provisioner eso secret delete --name <name> --namespace <namespace>")
	assert.NotContains(t, hints, "Delete the other objects: kubectl delete <resource.group> <name> [-n <namespace>]")
}

// Objects already deleting, or a release that is not deployed, may have no
// controller to clear them, so the hints point at recovery instead of deletion.
func Test_uninstallESOChart_StuckObjectsPointAtRecovery(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	tests := []struct {
		name     string
		status   release.Status
		deleting bool
	}{
		{name: "object already deleting", status: release.StatusDeployed, deleting: true},
		{name: "release failed", status: release.StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().List(spec.Namespace, false).Return([]*release.Release{
				esoRelease(spec.Release, spec.Namespace, esoChartName, tt.status),
			}, nil)
			k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
				externalSecretKind.groupKind(): {esoObj(externalSecretKind, "team-a", "db", tt.deleting)},
			}}

			_, err := uninstallESOChart(context.Background(), hm, k, spec)
			hints := requireReason(t, err, reasons.PreconditionNotMet)
			assert.Contains(t, hints, "If it is not running, there is no CLI repair for this release yet: do not uninstall over it; see "+esoRecoveryDoc)
			assert.NotContains(t, hints, "  sudo solo-provisioner eso secret delete --name <name> --namespace <namespace>")
			if tt.deleting {
				assert.Contains(t, err.Error(), "team-a/db (ExternalSecret, deleting)")
			}
		})
	}

	// On a release a recovery installed, the way out is to finish that recovery.
	t.Run("recovering release", func(t *testing.T) {
		rel := esoRelease(spec.Release, spec.Namespace, esoChartName, release.StatusDeployed)
		rel.Labels = map[string]string{esoRecoveryLabel: "draining"}
		hm := helm.NewMockManager(gomock.NewController(t))
		hm.EXPECT().List(spec.Namespace, false).Return([]*release.Release{rel}, nil)
		k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
			externalSecretKind.groupKind(): {esoObj(externalSecretKind, "team-a", "db", true)},
		}}

		_, err := uninstallESOChart(context.Background(), hm, k, spec)
		hints := requireReason(t, err, reasons.PreconditionNotMet)
		assert.Contains(t, hints, "Finish it: sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets")
		assert.NotContains(t, hints, "If it is not running, there is no CLI repair for this release yet: do not uninstall over it; see "+esoRecoveryDoc)
	})
}

func Test_uninstallESOChart_ListErrorRefuses(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil)

	_, err = uninstallESOChart(context.Background(), hm, &fakeESOCluster{listErr: errors.New("forbidden")}, spec)
	requireReason(t, err, reasons.K8sAPIFailed)
	assert.True(t, errorx.IsOfType(err, errorx.ExternalError))
}

// An empty namespace skips before anything is listed.
func Test_uninstallESOChart_NoReleaseListsNothing(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return(nil, nil)

	rel, err := uninstallESOChart(context.Background(), hm, &fakeESOCluster{listErr: errors.New("must not list")}, spec)
	require.NoError(t, err)
	assert.Nil(t, rel)
}

func Test_summarizeESOObjects_Caps(t *testing.T) {
	var objs []esoObject
	for i := 0; i < esoMaxListed+3; i++ {
		objs = append(objs, esoObject{kind: externalSecretKind, namespace: "ns", name: "es"})
	}
	assert.Contains(t, summarizeESOObjects(objs), "and 3 more (full list in the log)")
	assert.NotContains(t, summarizeESOObjects(objs[:esoMaxListed]), "more")
}

// A failed uninstall that purged the release and left the ExternalSecret CRD
// terminating gets recovery hints, not Helm ones.
func Test_uninstallESOChart_FailureHintsFollowTheState(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	tests := []struct {
		name      string
		after     []*release.Release
		k         *fakeESOCluster
		wantHint  string
		wantInMsg string
	}{
		{
			name:      "partial uninstall",
			k:         &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}},
			wantHint:  "Recover with " + esoRecoveryDoc,
			wantInMsg: "partial uninstall",
		},
		{
			name:     "release still there",
			after:    deployedESO(spec),
			k:        &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}},
			wantHint: "  helm list -a -n external-secrets",
		},
		{
			name:     "CRD read fails",
			k:        &fakeESOCluster{crdErr: errors.New("timeout")},
			wantHint: "  helm list -a -n external-secrets",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			gomock.InOrder(
				hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
				hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(errors.New("context deadline exceeded")),
				hm.EXPECT().List(spec.Namespace, false).Return(tt.after, nil),
			)

			_, err := uninstallESOChart(context.Background(), hm, tt.k, spec)
			hints := requireReason(t, err, reasons.PreconditionNotMet)
			assert.Contains(t, hints, tt.wantHint)
			assert.Contains(t, err.Error(), tt.wantInMsg)
		})
	}
}

func Test_checkOrphanedESOCRDs_PartialUninstall(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	terminating := map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}
	stuckES := map[schema.GroupKind][]unstructured.Unstructured{
		externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", true)},
	}

	tests := []struct {
		name      string
		k         *fakeESOCluster
		recover   bool
		wantErr   bool
		wantReas  errx.Reason
		wantHint  string
		wantInMsg string
	}{
		{
			name:      "without the flag refuses and names it",
			k:         &fakeESOCluster{crds: terminating},
			wantErr:   true,
			wantReas:  reasons.PreconditionNotMet,
			wantHint:  "  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets",
			wantInMsg: "stopped partway",
		},
		{
			name: "recovery refuses while a Secret is still owned",
			k: &fakeESOCluster{crds: terminating, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta(),
			}},
			recover:   true,
			wantErr:   true,
			wantReas:  reasons.PreconditionNotMet,
			wantHint:  `  kubectl patch secret grafana-alloy-secrets -n grafana-alloy --type=json -p '[{"op":"test","path":"/metadata/ownerReferences/0/uid","value":"es-uid"},{"op":"remove","path":"/metadata/ownerReferences/0"}]'`,
			wantInMsg: "Secret grafana-alloy/grafana-alloy-secrets is still owned",
		},
		{
			name: "recovery goes ahead once Secrets are detached",
			k: &fakeESOCluster{crds: terminating, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": {"labels": map[string]interface{}{"reconcile.external-secrets.io/managed": "true"}},
			}},
			recover: true,
		},
		{
			name:    "recovery goes ahead when the target Secret is gone",
			k:       &fakeESOCluster{crds: terminating, objects: stuckES},
			recover: true,
		},
		{
			name:     "recovery with an unreadable Secret refuses",
			k:        &fakeESOCluster{crds: terminating, objects: stuckES, secretErr: errors.New("forbidden")},
			recover:  true,
			wantErr:  true,
			wantReas: reasons.K8sAPIFailed,
		},
		{
			name: "recovery does not apply to a healthy orphan",
			k: &fakeESOCluster{crds: map[string]fakeCRD{
				esoExternalSecretCRD:                         {terminating: true},
				"clusterexternalsecrets.external-secrets.io": {},
			}},
			recover:  true,
			wantErr:  true,
			wantReas: reasons.PreconditionNotMet,
			wantHint: "--recover-partial-uninstall only applies after a partial uninstall, when every External Secrets Operator CRD is being deleted; see " + esoRecoveryDoc,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().ListAll().Return(nil, nil)

			_, err := checkOrphanedESOCRDs(context.Background(), hm, tt.k, spec, tt.recover)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			hints := requireReason(t, err, tt.wantReas)
			if tt.wantHint != "" {
				assert.Contains(t, hints, tt.wantHint)
			}
			assert.Contains(t, err.Error(), tt.wantInMsg)
		})
	}
}

// The healthy-orphan hints name only the CRDs found, and never suggest deleting
// a CRD.
func Test_checkOrphanedESOCRDs_HintsNameOnlyFoundCRDs(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().ListAll().Return(nil, nil)
	k := &fakeESOCluster{crds: map[string]fakeCRD{"clustersecretstores.external-secrets.io": {}}}

	_, err = checkOrphanedESOCRDs(context.Background(), hm, k, spec, false)
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, hints, "If they are left over from a removed installation, see what still uses them: kubectl get clustersecretstores.external-secrets.io -A")
	for _, h := range hints {
		assert.NotContains(t, h, "kubectl delete crd")
	}
}

func Test_externalSecretOwnerRef(t *testing.T) {
	index, uid, ok := externalSecretOwnerRef(ownedSecretMeta())
	assert.True(t, ok)
	assert.Equal(t, 0, index)
	assert.Equal(t, "es-uid", uid)

	_, _, ok = externalSecretOwnerRef(map[string]interface{}{})
	assert.False(t, ok)

	deployment := map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "uid": "d-uid"}
	_, _, ok = externalSecretOwnerRef(map[string]interface{}{"ownerReferences": []interface{}{deployment}})
	assert.False(t, ok)

	// Another owner first: the ExternalSecret's index is reported, so the patch
	// removes only that entry.
	both := ownedSecretMeta()
	both["ownerReferences"] = append([]interface{}{deployment}, both["ownerReferences"].([]interface{})...)
	index, uid, ok = externalSecretOwnerRef(both)
	assert.True(t, ok)
	assert.Equal(t, 1, index)
	assert.Equal(t, "es-uid", uid)
}

// The target name defaults to the ExternalSecret's name; spec.target.name
// overrides it.
func Test_ownedESOSecrets_TargetName(t *testing.T) {
	named := esoObj(externalSecretKind, "ns", "es", true)
	require.NoError(t, unstructured.SetNestedField(named.Object, "custom", "spec", "target", "name"))
	k := &fakeESOCluster{
		objects: map[schema.GroupKind][]unstructured.Unstructured{
			externalSecretKind.groupKind(): {esoObj(externalSecretKind, "ns", "plain", true), named},
		},
		secrets: map[string]map[string]interface{}{"ns/plain": ownedSecretMeta(), "ns/custom": ownedSecretMeta()},
	}

	owned, patches, err := ownedESOSecrets(context.Background(), k)
	require.NoError(t, err)
	assert.Equal(t, []string{"ns/plain", "ns/custom"}, owned)
	require.Len(t, patches, 2)
	assert.Contains(t, patches[1], "kubectl patch secret custom -n ns ")
}

// expectESOInstall expects one Helm install of the ESO chart, with Wait as given,
// returning err.
func expectESOInstall(t *testing.T, hm *helm.MockManager, spec *helmChartSpec, wait bool, err error) *gomock.Call {
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).
		Return("/tmp/chart.tgz", nil)
	return hm.EXPECT().InstallChart(gomock.Any(), spec.Release, "/tmp/chart.tgz", "", spec.Namespace, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.InstallChartOptions) (*release.Release, error) {
			assert.Equal(t, wait, opts.Wait, "Wait")
			assert.Equal(t, wait, opts.Atomic, "Atomic")
			return nil, err
		})
}

// A recovery installs without waiting, waits for the old CRDs to go, then
// uninstalls and installs again with Helm's wait, and succeeds with a warning.
func Test_recoverESOInstall_ReinstallsOnceTheOldCRDsAreGone(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	k := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: 2}

	hm := helm.NewMockManager(gomock.NewController(t))
	gomock.InOrder(
		expectESOInstall(t, hm, spec, false, nil),
		hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
		hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
		expectESOInstall(t, hm, spec, true, nil),
	)

	warning, err := recoverESOInstall(context.Background(), hm, k, spec, []string{esoExternalSecretCRD}, nil, time.Second, time.Millisecond)
	require.NoError(t, err)
	assert.Contains(t, warning, "recreate them")
	assert.Contains(t, warning, esoRecoveryDoc)
}

// A rerun over a release a failed recovery left installed skips the first
// install and still reinstalls.
func Test_recoverESOInstall_ReleaseAlreadyInstalled(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	k := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: 1}

	hm := helm.NewMockManager(gomock.NewController(t))
	gomock.InOrder(
		hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil),
		hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
		hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
		expectESOInstall(t, hm, spec, true, nil),
	)

	_, err = recoverESOInstall(context.Background(), hm, k, spec, []string{esoExternalSecretCRD}, nil, time.Second, time.Millisecond)
	require.NoError(t, err)
}

// A resumed recovery over a release that lost some CRDs while others still
// delete re-applies the release first, without Helm's wait or atomic, so the
// controller has every CRD it needs to drain the rest.
func Test_recoverESOInstall_ResumeReappliesMissingCRDs(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	k := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: 1}

	hm := helm.NewMockManager(gomock.NewController(t))
	gomock.InOrder(
		hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil),
		hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil),
		hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).
			Return("/tmp/chart.tgz", nil),
		hm.EXPECT().UpgradeChart(gomock.Any(), spec.Release, "/tmp/chart.tgz", "", spec.Namespace, gomock.Any()).
			DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.UpgradeChartOptions) (*release.Release, error) {
				assert.False(t, opts.Wait)
				assert.False(t, opts.Atomic)
				assert.Equal(t, "draining", opts.Labels[esoRecoveryLabel])
				return nil, nil
			}),
		hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
		hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
		expectESOInstall(t, hm, spec, true, nil),
	)

	_, err = recoverESOInstall(context.Background(), hm, k, spec, []string{esoExternalSecretCRD},
		[]string{"pushsecrets.external-secrets.io"}, time.Second, time.Millisecond)
	require.NoError(t, err)
}

func Test_recoverESOInstall_Failures(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	rerun := "  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets"
	deleting := func(goneAfter int) *fakeESOCluster {
		return &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: goneAfter}
	}

	tests := []struct {
		name     string
		k        *fakeESOCluster
		expect   func(t *testing.T, hm *helm.MockManager)
		wantHint string
	}{
		{
			name:     "recovery install fails: remove the release and rerun",
			k:        deleting(1),
			expect:   func(t *testing.T, hm *helm.MockManager) { expectESOInstall(t, hm, spec, false, errors.New("boom")) },
			wantHint: rerun,
		},
		{
			name: "old CRDs never go: no uninstall, rerun",
			k:    deleting(0),
			expect: func(t *testing.T, hm *helm.MockManager) {
				expectESOInstall(t, hm, spec, false, nil)
			},
			wantHint: rerun,
		},
		{
			name: "final install fails: rerun the recovery",
			k:    deleting(1),
			expect: func(t *testing.T, hm *helm.MockManager) {
				gomock.InOrder(
					expectESOInstall(t, hm, spec, false, nil),
					hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
					hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
					expectESOInstall(t, hm, spec, true, errors.New("boom")),
				)
			},
			wantHint: rerun,
		},
		{
			name: "uninstall refuses: the release stays, nothing installed again",
			k: func() *fakeESOCluster {
				k := deleting(1)
				k.objects = map[schema.GroupKind][]unstructured.Unstructured{
					externalSecretKind.groupKind(): {esoObj(externalSecretKind, "ns", "new", false)},
				}
				return k
			}(),
			expect: func(t *testing.T, hm *helm.MockManager) {
				gomock.InOrder(
					expectESOInstall(t, hm, spec, false, nil),
					hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
				)
			},
			wantHint: "  sudo solo-provisioner eso operator uninstall --namespace external-secrets",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			tt.expect(t, hm)

			_, err := recoverESOInstall(context.Background(), hm, tt.k, spec, []string{esoExternalSecretCRD}, nil, 20*time.Millisecond, time.Millisecond)
			hints := requireReason(t, err, reasons.PreconditionNotMet)
			assert.Contains(t, hints, tt.wantHint)
		})
	}
}

// The refusal without the flag shows what the recovery would refuse over and
// what it would clean up, before the operator chooses the flag.
func Test_checkPartialUninstallRecovery_RefusalPreviewsTheRecovery(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	k := &fakeESOCluster{
		objects: map[schema.GroupKind][]unstructured.Unstructured{
			externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", true)},
			pushSecretKind.groupKind():     {pushSecret("drop", "Delete")},
			generatorStateKind.groupKind(): {esoObj(generatorStateKind, "team-a", "token", true)},
		},
		secrets: map[string]map[string]interface{}{"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta()},
	}

	_, err = checkPartialUninstallRecovery(context.Background(), k, spec, []string{esoExternalSecretCRD}, false)
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, err.Error(), "stopped partway")
	assert.Contains(t, err.Error(), "Secret grafana-alloy/grafana-alloy-secrets is still owned")
	assert.Contains(t, err.Error(), "PushSecret team-a/drop has deletionPolicy Delete")
	assert.Contains(t, err.Error(), "finish deleting GeneratorState team-a/token")
	assert.Contains(t, hints, `  kubectl patch secret grafana-alloy-secrets -n grafana-alloy --type=json -p '[{"op":"test","path":"/metadata/ownerReferences/0/uid","value":"es-uid"},{"op":"remove","path":"/metadata/ownerReferences/0"}]'`)
	assert.Contains(t, hints, "  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets")

	// Nothing to keep or clean up: the refusal only names the flag.
	_, err = checkPartialUninstallRecovery(context.Background(), &fakeESOCluster{}, spec, []string{esoExternalSecretCRD}, false)
	hints = requireReason(t, err, reasons.PreconditionNotMet)
	assert.NotContains(t, err.Error(), "GeneratorState")
	assert.NotContains(t, hints, "Detach each Secret you need to keep:")
}

// A PushSecret a ClusterPushSecret created is rewritten from it, so the
// uninstall refusal points at the ClusterPushSecret.
func Test_uninstallESOChart_PushSecretHintNamesClusterPushSecrets(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	ps := pushSecret("drop", "Delete")
	ps.SetDeletionTimestamp(nil)

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil)
	k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{pushSecretKind.groupKind(): {ps}}}

	_, err = uninstallESOChart(context.Background(), hm, k, spec)
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, hints, "  kubectl get clusterpushsecrets.external-secrets.io")
}

func Test_waitESOCRDsGone(t *testing.T) {
	names := []string{esoExternalSecretCRD}
	gone := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: 3}
	require.NoError(t, waitESOCRDsGone(context.Background(), gone, names, time.Second, time.Millisecond))
	assert.Len(t, gone.lookups, 4)

	stuck := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}}
	err := waitESOCRDsGone(context.Background(), stuck, names, 20*time.Millisecond, time.Millisecond)
	assert.True(t, errorx.IsOfType(err, errorx.TimeoutElapsed), "timeout: %v", err)

	unreadable := &fakeESOCluster{crdErr: errors.New("forbidden")}
	err = waitESOCRDsGone(context.Background(), unreadable, names, 20*time.Millisecond, time.Millisecond)
	assert.True(t, errorx.IsOfType(err, errorx.TimeoutElapsed), "unreadable: %v", err)
	assert.ErrorContains(t, err, "could not confirm")
	assert.ErrorContains(t, err, "forbidden")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = waitESOCRDsGone(ctx, stuck, names, time.Minute, time.Millisecond)
	assert.True(t, errorx.IsOfType(err, errorx.Interrupted), "cancelled: %v", err)
}

func Test_partialUninstallWarning(t *testing.T) {
	assert.Contains(t,
		partialUninstallWarning(context.Background(), &fakeESOCluster{crds: map[string]fakeCRD{"pushsecrets.external-secrets.io": {terminating: true}}}),
		"pushsecrets.external-secrets.io")
	assert.Contains(t,
		partialUninstallWarning(context.Background(), &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}}),
		esoRecoveryDoc)
	assert.Empty(t, partialUninstallWarning(context.Background(), &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {}}}))
	assert.Empty(t, partialUninstallWarning(context.Background(), &fakeESOCluster{}))
	assert.Empty(t, partialUninstallWarning(context.Background(), &fakeESOCluster{crdErr: errors.New("boom")}))
}

func Test_uninstallSkipMetadata(t *testing.T) {
	md := uninstallSkipMetadata(context.Background(), &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}})
	assert.Contains(t, md[models.ReportMetaWarning], esoRecoveryDoc)
	assert.Nil(t, uninstallSkipMetadata(context.Background(), &fakeESOCluster{}))
}

func Test_resolveESOClusterReader_FailsClosed(t *testing.T) {
	_, err := resolveESOClusterReader(context.Background(), func(context.Context) (esoClusterReader, error) {
		return nil, errors.New("no kubeconfig")
	})
	requireReason(t, err, reasons.K8sAPIFailed)
}

func pushSecret(name, policy string) unstructured.Unstructured {
	u := esoObj(pushSecretKind, "team-a", name, true)
	if policy != "" {
		_ = unstructured.SetNestedField(u.Object, policy, "spec", "deletionPolicy")
	}
	return u
}

// A forced recovery must not let the controller delete pushed secrets in the
// remote store; GeneratorStates only warn.
func Test_checkPartialUninstallRecovery_PushSecretsAndGeneratorStates(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	terminating := []string{"pushsecrets.external-secrets.io"}

	k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
		pushSecretKind.groupKind(): {pushSecret("keep", "None"), pushSecret("drop", "Delete")},
	}}
	_, err = checkPartialUninstallRecovery(context.Background(), k, spec, terminating, true)
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, err.Error(), "team-a/drop")
	assert.NotContains(t, err.Error(), "team-a/keep")
	assert.Contains(t, hints, `  kubectl patch pushsecrets.external-secrets.io drop -n team-a --type=merge -p '{"spec":{"deletionPolicy":"None"}}'`)

	k = &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
		pushSecretKind.groupKind():     {pushSecret("keep", "None")},
		generatorStateKind.groupKind(): {esoObj(generatorStateKind, "team-a", "token", true)},
	}}
	warning, err := checkPartialUninstallRecovery(context.Background(), k, spec, terminating, true)
	require.NoError(t, err)
	assert.Contains(t, warning, "team-a/token")
}

func Test_checkRollbackLeavesNoFinalizedObjects(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	require.NoError(t, checkRollbackLeavesNoFinalizedObjects(context.Background(), &fakeESOCluster{}, spec))

	k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
		externalSecretKind.groupKind(): {esoObj(externalSecretKind, "ns", "es", true)},
	}}
	err = checkRollbackLeavesNoFinalizedObjects(context.Background(), k, spec)
	requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, err.Error(), "rollback left release")
}

// A recovery install skips Helm's wait and atomic rollback: the adopted CRDs
// finish deleting during the install, which Helm would read as a failure.
func Test_installESOChart_RecoveryInstallsWithoutWaitOrAtomic(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(false, nil)
	hm.EXPECT().AddRepo(spec.RepoAlias, spec.Repo, gomock.Any()).Return(nil, nil)
	hm.EXPECT().PullAndVerify(gomock.Any(), gomock.Any(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum).
		Return("/tmp/chart.tgz", nil)
	hm.EXPECT().InstallChart(gomock.Any(), spec.Release, "/tmp/chart.tgz", "", spec.Namespace, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, opts helm.InstallChartOptions) (*release.Release, error) {
			assert.False(t, opts.Atomic)
			assert.False(t, opts.Wait)
			assert.True(t, opts.CreateNamespace)
			assert.Equal(t, "draining", opts.Labels[esoRecoveryLabel], "a rerun must know the recovery has not finished")
			return nil, nil
		})

	installed, err := installESOChart(context.Background(), hm, spec, true)
	require.NoError(t, err)
	assert.True(t, installed)
}

func Test_esoRecoveryGaps(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	drained := allESOCRDs(false)
	delete(drained, "pushsecrets.external-secrets.io")

	tests := []struct {
		name       string
		rels       []*release.Release
		k          *fakeESOCluster
		wantTerm   []string
		wantMiss   []string
		wantResume bool
	}{
		{
			name:       "partial uninstall: a CRD still deleting, no release",
			k:          &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}},
			wantTerm:   []string{esoExternalSecretCRD},
			wantResume: true,
		},
		{
			name:       "a recovery whose drain finished: the recovering release lacks a CRD",
			rels:       recoveringESO(spec, esoCRDs...),
			k:          &fakeESOCluster{crds: drained},
			wantMiss:   []string{"pushsecrets.external-secrets.io"},
			wantResume: true,
		},
		{
			name:     "a CRD removed from a release no recovery touched",
			rels:     deployedESOWithCRDs(spec, esoCRDs...),
			k:        &fakeESOCluster{crds: drained},
			wantMiss: []string{"pushsecrets.external-secrets.io"},
		},
		{name: "healthy", rels: deployedESOWithCRDs(spec, esoCRDs...), k: &fakeESOCluster{crds: allESOCRDs(false)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().List(spec.Namespace, false).Return(tt.rels, nil)

			terminating, missing, resume, err := esoRecoveryGaps(context.Background(), hm, tt.k, spec)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTerm, terminating)
			assert.Equal(t, tt.wantMiss, missing)
			assert.Equal(t, tt.wantResume, resume)
		})
	}

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return(nil, nil)
	_, _, _, err = esoRecoveryGaps(context.Background(), hm, &fakeESOCluster{crdErr: errors.New("boom")}, spec)
	requireReason(t, err, reasons.K8sAPIFailed)
}

// A custom install that left an optional CRD out lacks nothing: only the CRDs
// the release's manifest declares count as missing.
func Test_releaseCRDs(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	got, err := releaseCRDs(deployedESOWithCRDs(spec, esoExternalSecretCRD, "secretstores.external-secrets.io")[0])
	require.NoError(t, err)
	assert.Equal(t, []string{esoExternalSecretCRD, "secretstores.external-secrets.io"}, got)

	got, err = releaseCRDs(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	bad := deployedESO(spec)[0]
	bad.Manifest = "kind: [unterminated"
	_, err = releaseCRDs(bad)
	requireReason(t, err, reasons.PreconditionNotMet)
}

// The install step's work: a plain install, an already-installed release, and a
// recovery, by what the cluster shows when the step runs.
func Test_installOrRecoverESO(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	provider := func(k *fakeESOCluster) esoClusterReaderProvider {
		return func(context.Context) (esoClusterReader, error) { return k, nil }
	}
	run := func(hm helm.Manager, recoverPartial bool, k *fakeESOCluster) (map[string]string, error) {
		return installOrRecoverESO(context.Background(), hm, spec, recoverPartial, provider(k), time.Second, time.Millisecond)
	}
	drained := allESOCRDs(false)
	delete(drained, "pushsecrets.external-secrets.io")

	t.Run("plain install", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		gomock.InOrder(
			hm.EXPECT().ListAll().Return(nil, nil),
			expectESOInstall(t, hm, spec, true, nil),
		)
		meta, err := run(hm, false, &fakeESOCluster{})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{InstalledByThisStep: "true"}, meta)
	})

	t.Run("already installed", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		hm.EXPECT().ListAll().Return(deployedESOWithCRDs(spec, esoCRDs...), nil)
		hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)
		meta, err := run(hm, false, &fakeESOCluster{crds: allESOCRDs(false)})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{AlreadyInstalled: "true"}, meta)
	})

	t.Run("flag on a healthy release is a plain install", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		hm.EXPECT().ListAll().Return(deployedESOWithCRDs(spec, esoCRDs...), nil)
		hm.EXPECT().List(spec.Namespace, false).Return(deployedESOWithCRDs(spec, esoCRDs...), nil)
		hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)
		meta, err := run(hm, true, &fakeESOCluster{crds: allESOCRDs(false)})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{AlreadyInstalled: "true"}, meta)
	})

	t.Run("flag on a release with a CRD removed by hand: no recovery", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		hm.EXPECT().ListAll().Return(deployedESOWithCRDs(spec, esoCRDs...), nil)
		hm.EXPECT().List(spec.Namespace, false).Return(deployedESOWithCRDs(spec, esoCRDs...), nil)
		hm.EXPECT().IsInstalled(spec.Release, spec.Namespace).Return(true, nil)
		meta, err := run(hm, true, &fakeESOCluster{crds: drained})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{AlreadyInstalled: "true"}, meta)
	})

	t.Run("recovery over a partial uninstall", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		gomock.InOrder(
			hm.EXPECT().ListAll().Return(nil, nil),
			hm.EXPECT().List(spec.Namespace, false).Return(nil, nil),
			expectESOInstall(t, hm, spec, false, nil),
			hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil),
			hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
			expectESOInstall(t, hm, spec, true, nil),
		)
		k := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: 2}
		meta, err := run(hm, true, k)
		require.NoError(t, err)
		assert.Equal(t, "true", meta[InstalledByThisStep])
		assert.Contains(t, meta[models.ReportMetaWarning], "recreate them")
	})

	// The rerun after a recovery whose wait timed out and whose drain then
	// finished: nothing terminating, the recovering release lacks a CRD. It
	// reinstalls without a first install or a wait.
	t.Run("rerun after the drain finished", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		rels := recoveringESO(spec, esoCRDs...)
		gomock.InOrder(
			hm.EXPECT().ListAll().Return(rels, nil),
			hm.EXPECT().List(spec.Namespace, false).Return(rels, nil),
			hm.EXPECT().List(spec.Namespace, false).Return(rels, nil),
			hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil),
			expectESOInstall(t, hm, spec, true, nil),
		)
		meta, err := run(hm, true, &fakeESOCluster{crds: drained})
		require.NoError(t, err)
		assert.Contains(t, meta[models.ReportMetaWarning], "Recovered")
	})

	// With --continue-on-error the step runs after a precheck refusal; it must
	// refuse again before any Helm change. The mock fails the test on any call
	// other than ListAll.
	t.Run("refuses again over data the recovery would destroy", func(t *testing.T) {
		k := &fakeESOCluster{
			crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}},
			objects: map[schema.GroupKind][]unstructured.Unstructured{
				externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", true)},
			},
			secrets: map[string]map[string]interface{}{"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta()},
		}
		hm := helm.NewMockManager(gomock.NewController(t))
		hm.EXPECT().ListAll().Return(nil, nil)
		_, err := run(hm, true, k)
		requireReason(t, err, reasons.PreconditionNotMet)
		assert.Contains(t, err.Error(), "would destroy data")
	})

	t.Run("refuses a plain install over a partial uninstall", func(t *testing.T) {
		hm := helm.NewMockManager(gomock.NewController(t))
		hm.EXPECT().ListAll().Return(nil, nil)
		_, err := run(hm, false, &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}})
		requireReason(t, err, reasons.PreconditionNotMet)
		assert.Contains(t, err.Error(), "stopped partway")
	})
}

// A failed read while building the preview keeps the state in the message.
func Test_recoveryNeededError_ReadFailureKeepsTheDiagnosis(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	err = recoveryNeededError(context.Background(), &fakeESOCluster{listErr: errors.New("forbidden")}, spec, "a previous uninstall stopped partway")
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, err.Error(), "a previous uninstall stopped partway")
	assert.Contains(t, err.Error(), "forbidden")
	assert.Contains(t, hints, "  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets")
}

// Both blockers are named in one refusal, so the operator fixes them in one pass.
func Test_checkPartialUninstallRecovery_NamesEveryBlocker(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	k := &fakeESOCluster{
		objects: map[schema.GroupKind][]unstructured.Unstructured{
			externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", true)},
			pushSecretKind.groupKind():     {pushSecret("drop", "Delete")},
		},
		secrets: map[string]map[string]interface{}{"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta()},
	}

	_, err = checkPartialUninstallRecovery(context.Background(), k, spec, []string{esoExternalSecretCRD}, true)
	hints := requireReason(t, err, reasons.PreconditionNotMet)
	assert.Contains(t, err.Error(), "Secret grafana-alloy/grafana-alloy-secrets is still owned")
	assert.Contains(t, err.Error(), "PushSecret team-a/drop has deletionPolicy Delete")
	assert.Contains(t, hints, "Detach each Secret you need to keep:")
	assert.Contains(t, hints, "Keep each remote secret by setting its deletionPolicy to None:")
}

func Test_crdsPhrase(t *testing.T) {
	assert.Equal(t, "CRD a is", crdsPhrase([]string{"a"}))
	assert.Equal(t, "CRDs a, b are", crdsPhrase([]string{"a", "b"}))
}

// An installed ESO release must still have the CRDs its manifest declares: a
// recovery that failed or was interrupted leaves one whose old CRDs are
// deleting or gone.
func Test_checkOrphanedESOCRDs_InstalledReleaseCRDs(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	optionalMissing := allESOCRDs(false)
	delete(optionalMissing, "pushsecrets.external-secrets.io")
	withoutPushSecrets := []string{}
	for _, name := range esoCRDs {
		if name != "pushsecrets.external-secrets.io" {
			withoutPushSecrets = append(withoutPushSecrets, name)
		}
	}
	stillDeleting := allESOCRDs(false)
	stillDeleting[esoExternalSecretCRD] = fakeCRD{terminating: true}
	stuckES := map[schema.GroupKind][]unstructured.Unstructured{
		externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", true)},
	}

	tests := []struct {
		name       string
		k          *fakeESOCluster
		crds       []string // the release manifest's; nil = every esoCRD
		recovering bool     // the release carries esoRecoveryLabel
		recover    bool
		wantErr    bool
		wantHint   string
		wantMsg    string
		wantWarn   string
	}{
		{name: "every CRD present", k: &fakeESOCluster{crds: allESOCRDs(false)}},
		{name: "a CRD the release never declared is absent", k: &fakeESOCluster{crds: optionalMissing}, crds: withoutPushSecrets},
		{
			name:     "a CRD missing from a release no recovery touched only warns",
			k:        &fakeESOCluster{crds: optionalMissing},
			wantWarn: "lacks CRD pushsecrets.external-secrets.io that it installed",
		},
		{
			name:     "same with the flag: no recovery",
			k:        &fakeESOCluster{crds: optionalMissing},
			recover:  true,
			wantWarn: "lacks CRD pushsecrets.external-secrets.io that it installed",
		},
		{
			name:       "a recovering release lacks a CRD, no flag",
			k:          &fakeESOCluster{crds: optionalMissing},
			recovering: true,
			wantErr:    true,
			wantHint:   "  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets",
			wantMsg:    "CRD pushsecrets.external-secrets.io is missing: a recovery from a partial uninstall has not finished",
		},
		{name: "a recovering release lacks a CRD, flag", k: &fakeESOCluster{crds: optionalMissing}, recovering: true, recover: true},
		{
			name: "a recovering release lacks a CRD, flag, Secret still owned",
			k: &fakeESOCluster{crds: optionalMissing, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta(),
			}},
			recovering: true,
			recover:    true,
			wantErr:    true,
			wantHint:   "Detach each Secret you need to keep:",
		},
		{
			name:     "a CRD still deleting, no flag",
			k:        &fakeESOCluster{crds: stillDeleting},
			wantErr:  true,
			wantHint: "  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace external-secrets",
		},
		{
			name: "a CRD still deleting, flag, Secret still owned",
			k: &fakeESOCluster{crds: stillDeleting, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta(),
			}},
			recover:  true,
			wantErr:  true,
			wantHint: "Detach each Secret you need to keep:",
		},
		{name: "a CRD still deleting, flag, data detached", k: &fakeESOCluster{crds: stillDeleting, objects: stuckES}, recover: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crds := tt.crds
			if crds == nil {
				crds = esoCRDs
			}
			rels := deployedESOWithCRDs(spec, crds...)
			if tt.recovering {
				rels = recoveringESO(spec, crds...)
			}
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().ListAll().Return(rels, nil)

			warning, err := checkOrphanedESOCRDs(context.Background(), hm, tt.k, spec, tt.recover)
			if !tt.wantErr {
				require.NoError(t, err)
				if tt.wantWarn != "" {
					assert.Contains(t, warning, tt.wantWarn)
				}
				return
			}
			hints := requireReason(t, err, reasons.PreconditionNotMet)
			assert.Contains(t, hints, tt.wantHint)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

// A PushSecret with deletionPolicy None has no finalizer, so it does not block.
func Test_uninstallESOChart_IgnoresObjectsWithoutFinalizers(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	ps := pushSecret("keep", "None")
	ps.SetFinalizers(nil)
	ps.SetDeletionTimestamp(nil)

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return(deployedESO(spec), nil)
	hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)
	k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{pushSecretKind.groupKind(): {ps}}}

	_, err = uninstallESOChart(context.Background(), hm, k, spec)
	require.NoError(t, err)
}

// A stalled record whose objects are all already deleting is uninstalled, so
// install --recover-partial-uninstall can recover; one with an object not yet deleting refuses.
func Test_uninstallESOChart_StalledReleaseWithDeletingObjects(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	for _, status := range []release.Status{release.StatusUninstalling, release.StatusFailed, release.StatusPendingInstall} {
		t.Run(string(status), func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().List(spec.Namespace, false).Return([]*release.Release{
				esoRelease(spec.Release, spec.Namespace, esoChartName, status),
			}, nil)
			hm.EXPECT().UninstallChart(spec.Release, spec.Namespace).Return(nil)
			k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
				externalSecretKind.groupKind(): {esoObj(externalSecretKind, "ns", "a", true), esoObj(externalSecretKind, "ns", "b", true)},
			}}

			_, err := uninstallESOChart(context.Background(), hm, k, spec)
			require.NoError(t, err)
		})
	}

	hm := helm.NewMockManager(gomock.NewController(t))
	hm.EXPECT().List(spec.Namespace, false).Return([]*release.Release{
		esoRelease(spec.Release, spec.Namespace, esoChartName, release.StatusFailed),
	}, nil)
	k := &fakeESOCluster{objects: map[schema.GroupKind][]unstructured.Unstructured{
		externalSecretKind.groupKind(): {esoObj(externalSecretKind, "ns", "a", true), esoObj(externalSecretKind, "ns", "b", false)},
	}}
	_, err = uninstallESOChart(context.Background(), hm, k, spec)
	requireReason(t, err, reasons.PreconditionNotMet)
}
