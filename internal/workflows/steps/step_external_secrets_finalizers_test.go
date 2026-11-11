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
			map[string]interface{}{"apiVersion": "external-secrets.io/v1", "kind": "ExternalSecret", "name": "x"},
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
		force     bool
		wantErr   bool
		wantReas  errx.Reason
		wantHint  string
		wantInMsg string
	}{
		{
			name:      "without force refuses and names recovery",
			k:         &fakeESOCluster{crds: terminating},
			wantErr:   true,
			wantReas:  reasons.PreconditionNotMet,
			wantHint:  "  sudo solo-provisioner eso operator install --force --namespace external-secrets",
			wantInMsg: "stopped partway",
		},
		{
			name: "force refuses while a Secret is still owned",
			k: &fakeESOCluster{crds: terminating, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta(),
			}},
			force:     true,
			wantErr:   true,
			wantReas:  reasons.PreconditionNotMet,
			wantHint:  `  kubectl patch secret grafana-alloy-secrets -n grafana-alloy --type=json -p '[{"op":"remove","path":"/metadata/ownerReferences"}]'`,
			wantInMsg: "Secret grafana-alloy/grafana-alloy-secrets is still owned",
		},
		{
			name: "force goes ahead once Secrets are detached",
			k: &fakeESOCluster{crds: terminating, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": {"labels": map[string]interface{}{"reconcile.external-secrets.io/managed": "true"}},
			}},
			force: true,
		},
		{
			name:  "force goes ahead when the target Secret is gone",
			k:     &fakeESOCluster{crds: terminating, objects: stuckES},
			force: true,
		},
		{
			name:     "force with an unreadable Secret refuses",
			k:        &fakeESOCluster{crds: terminating, objects: stuckES, secretErr: errors.New("forbidden")},
			force:    true,
			wantErr:  true,
			wantReas: reasons.K8sAPIFailed,
		},
		{
			name: "force does not apply to a healthy orphan",
			k: &fakeESOCluster{crds: map[string]fakeCRD{
				esoExternalSecretCRD:                         {terminating: true},
				"clusterexternalsecrets.external-secrets.io": {},
			}},
			force:    true,
			wantErr:  true,
			wantReas: reasons.PreconditionNotMet,
			wantHint: "--force only applies after a partial uninstall, when every External Secrets Operator CRD is being deleted; see " + esoRecoveryDoc,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().ListAll().Return(nil, nil)

			_, err := checkOrphanedESOCRDs(context.Background(), hm, tt.k, spec, tt.force)
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

func Test_ownedByExternalSecret(t *testing.T) {
	assert.True(t, ownedByExternalSecret(ownedSecretMeta()))
	assert.False(t, ownedByExternalSecret(map[string]interface{}{}))
	assert.False(t, ownedByExternalSecret(map[string]interface{}{
		"ownerReferences": []interface{}{map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment"}},
	}))
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

	owned, err := ownedESOSecrets(context.Background(), k)
	require.NoError(t, err)
	assert.Equal(t, []string{"ns/plain", "ns/custom"}, owned)
}

func Test_finishPartialUninstall(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)

	tests := []struct {
		name     string
		k        *fakeESOCluster
		timeout  time.Duration
		wantErr  bool
		wantReas errx.Reason
		wantHint string
	}{
		{
			name: "every CRD present: nothing to finish",
			k:    &fakeESOCluster{crds: allESOCRDs(false)},
		},
		{
			name:     "CRD drains: reinstall next",
			k:        &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}, goneAfter: 1},
			timeout:  time.Second,
			wantErr:  true,
			wantReas: reasons.PreconditionNotMet,
			wantHint: "  sudo solo-provisioner eso operator uninstall --namespace external-secrets",
		},
		{
			name:     "CRD already gone: reinstall next",
			k:        &fakeESOCluster{},
			wantErr:  true,
			wantReas: reasons.PreconditionNotMet,
			wantHint: "  sudo solo-provisioner eso operator install --namespace external-secrets",
		},
		{
			name: "a CRD other than ExternalSecret still deleting",
			k: &fakeESOCluster{crds: map[string]fakeCRD{
				esoExternalSecretCRD:                             {},
				"secretstores.external-secrets.io":               {terminating: true},
				"clusterexternalsecrets.external-secrets.io":     {},
				"clustersecretstores.external-secrets.io":        {},
				"pushsecrets.external-secrets.io":                {},
				"generatorstates.generators.external-secrets.io": {},
			}},
			timeout:  50 * time.Millisecond,
			wantErr:  true,
			wantReas: reasons.PreconditionNotMet,
			wantHint: "  sudo solo-provisioner eso operator install --force --namespace external-secrets",
		},
		{
			name:     "CRD never drains",
			k:        &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}},
			timeout:  50 * time.Millisecond,
			wantErr:  true,
			wantReas: reasons.PreconditionNotMet,
			wantHint: "  sudo solo-provisioner eso operator install --force --namespace external-secrets",
		},
		{
			name:     "CRD read fails",
			k:        &fakeESOCluster{crdErr: errors.New("timeout")},
			wantErr:  true,
			wantReas: reasons.K8sAPIFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := finishPartialUninstall(context.Background(), tt.k, spec, tt.timeout, time.Millisecond)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			hints := requireReason(t, err, tt.wantReas)
			if tt.wantHint != "" {
				assert.Contains(t, hints, tt.wantHint)
			}
		})
	}
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
			return nil, nil
		})

	installed, err := installESOChart(context.Background(), hm, spec, true)
	require.NoError(t, err)
	assert.True(t, installed)
}

func Test_isESORecovery(t *testing.T) {
	provider := func(k *fakeESOCluster) esoClusterReaderProvider {
		return func(context.Context) (esoClusterReader, error) { return k, nil }
	}
	deleting := &fakeESOCluster{crds: map[string]fakeCRD{esoExternalSecretCRD: {terminating: true}}}

	got, err := isESORecovery(context.Background(), false, provider(deleting))
	require.NoError(t, err)
	assert.False(t, got, "only a forced install recovers")

	got, err = isESORecovery(context.Background(), true, provider(deleting))
	require.NoError(t, err)
	assert.True(t, got)

	got, err = isESORecovery(context.Background(), true, provider(&fakeESOCluster{crds: allESOCRDs(false)}))
	require.NoError(t, err)
	assert.False(t, got, "a forced install on a healthy cluster is a normal install")

	_, err = isESORecovery(context.Background(), true, provider(&fakeESOCluster{crdErr: errors.New("boom")}))
	requireReason(t, err, reasons.K8sAPIFailed)
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

// An installed ESO release must still have its CRDs: a recovery that failed or
// was interrupted leaves one whose old CRDs are deleting or gone.
func Test_checkOrphanedESOCRDs_InstalledReleaseCRDs(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	optionalMissing := allESOCRDs(false)
	delete(optionalMissing, "pushsecrets.external-secrets.io")
	requiredMissing := allESOCRDs(false)
	delete(requiredMissing, esoExternalSecretCRD)
	stillDeleting := allESOCRDs(false)
	stillDeleting[esoExternalSecretCRD] = fakeCRD{terminating: true}
	stuckES := map[schema.GroupKind][]unstructured.Unstructured{
		externalSecretKind.groupKind(): {esoObj(externalSecretKind, "grafana-alloy", "grafana-alloy-secrets", true)},
	}

	tests := []struct {
		name     string
		k        *fakeESOCluster
		force    bool
		wantErr  bool
		wantHint string
	}{
		{name: "every CRD present", k: &fakeESOCluster{crds: allESOCRDs(false)}},
		{name: "only an optional CRD missing", k: &fakeESOCluster{crds: optionalMissing}},
		{
			name:     "a required CRD missing",
			k:        &fakeESOCluster{crds: requiredMissing},
			force:    true,
			wantErr:  true,
			wantHint: "  sudo solo-provisioner eso operator uninstall --namespace external-secrets",
		},
		{
			name:     "a CRD still deleting, no force",
			k:        &fakeESOCluster{crds: stillDeleting},
			wantErr:  true,
			wantHint: "  sudo solo-provisioner eso operator install --force --namespace external-secrets",
		},
		{
			name: "a CRD still deleting, force, Secret still owned",
			k: &fakeESOCluster{crds: stillDeleting, objects: stuckES, secrets: map[string]map[string]interface{}{
				"grafana-alloy/grafana-alloy-secrets": ownedSecretMeta(),
			}},
			force:    true,
			wantErr:  true,
			wantHint: "Detach each Secret you need to keep:",
		},
		{name: "a CRD still deleting, force, data detached", k: &fakeESOCluster{crds: stillDeleting, objects: stuckES}, force: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := helm.NewMockManager(gomock.NewController(t))
			hm.EXPECT().ListAll().Return(deployedESO(spec), nil)

			_, err := checkOrphanedESOCRDs(context.Background(), hm, tt.k, spec, tt.force)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			hints := requireReason(t, err, reasons.PreconditionNotMet)
			assert.Contains(t, hints, tt.wantHint)
		})
	}
}

// A forced install on a healthy ESO without an optional CRD has nothing to finish.
func Test_finishPartialUninstall_OptionalCRDMissing(t *testing.T) {
	spec, err := resolveCatalogChart("external-secrets")
	require.NoError(t, err)
	crds := allESOCRDs(false)
	delete(crds, "clustersecretstores.external-secrets.io")

	require.NoError(t, finishPartialUninstall(context.Background(), &fakeESOCluster{crds: crds}, spec, time.Second, time.Millisecond))
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
// install --force can recover; one with an object not yet deleting refuses.
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
