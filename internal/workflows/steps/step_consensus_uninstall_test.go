// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// stuckDeleteClient accepts a delete but never removes the resource, like a capsule held by a finalizer.
type stuckDeleteClient struct {
	*fakeCapsuleClient
}

func (s *stuckDeleteClient) DeleteResource(_ context.Context, _, _, _, name string) (bool, error) {
	_, ok := s.existing[name]
	return ok, nil
}

// noCRDClient fails like the real client when the operator CRDs are absent.
type noCRDClient struct {
	*fakeCapsuleClient
}

func noKindMatch(kind string) error {
	return errorx.IllegalArgument.Wrap(&meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: kube.SoloOperatorGroup, Kind: kind}}, "failed to get REST mapping")
}

func (n *noCRDClient) DeleteResource(_ context.Context, _, kind, _, _ string) (bool, error) {
	return false, noKindMatch(kind)
}

func (n *noCRDClient) ResourceExists(_ context.Context, _, kind, _, _ string) (bool, error) {
	return false, noKindMatch(kind)
}

func (n *noCRDClient) List(ctx context.Context, kind kube.ResourceKind, ns string, opts kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	if kind == kube.KindPVC {
		return n.fakeCapsuleClient.List(ctx, kind, ns, opts) // a core kind, served without the operator CRDs
	}
	return nil, kerrors.NewNotFound(schema.GroupResource{Group: kube.SoloOperatorGroup, Resource: string(kind)}, "")
}

func noCRDProvider() CapsuleKubeProvider {
	c := &noCRDClient{&fakeCapsuleClient{}}
	return func(context.Context) (CapsuleKubeClient, error) { return c, nil }
}

// Deleting a CRD deletes its CRs, so every uninstall step must succeed without the CRDs.
func TestConsensusUninstallSteps_SucceedWithoutOperatorCRDs(t *testing.T) {
	for name, b := range map[string]automa.Builder{
		"pvcs":       ReleaseConsensusPVCs(uninstallInputs(), noCRDProvider()),
		"capsule":    DeleteConsensusCapsule(uninstallInputs(), noCRDProvider()),
		"config CRs": DeleteConsensusConfigCRs(uninstallInputs(), noCRDProvider()),
		"orbit":      DeleteOrbitIfLast(uninstallInputs(), noCRDProvider()),
	} {
		t.Run(name, func(t *testing.T) {
			rpt := runUninstallStep(t, b)
			require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
		})
	}
}

func uninstallInputs() models.ConsensusNodeInputs {
	return models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0}
}

func runUninstallStep(t *testing.T, b automa.Builder) *automa.Report {
	t.Helper()
	step, err := b.Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

// --- ReleaseConsensusPVCs ---

// pvcOwnedBy builds a PVC whose owners are the named ConsensusCapsules.
func pvcOwnedBy(name string, capsules ...string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetName(name)
	var refs []metav1.OwnerReference
	for _, c := range capsules {
		refs = append(refs, metav1.OwnerReference{
			APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
			Kind:       string(kube.KindConsensusCapsule),
			Name:       c,
		})
	}
	u.SetOwnerReferences(refs)
	return u
}

// The capsule delete garbage-collects every PVC it still owns, so this node's PVCs must be released first.
func TestReleaseConsensusPVCs_ReleasesOnlyThisNodesPVCs(t *testing.T) {
	capsule0 := models.ConsensusCapsuleName("orbit", 0)
	capsule1 := models.ConsensusCapsuleName("orbit", 1)
	fake := &fakeCapsuleClient{pvcs: []unstructured.Unstructured{
		pvcOwnedBy(capsule0+"-saved", capsule0),
		pvcOwnedBy("shared", capsule0, "other-owner"),
		pvcOwnedBy(capsule1+"-saved", capsule1),
		pvcOwnedBy("unowned"),
	}}

	rpt := runUninstallStep(t, ReleaseConsensusPVCs(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, []string{capsule0 + "-saved", "shared"}, fake.released)
	assert.Equal(t, "2", rpt.Metadata["releasedCount"])
	assert.Empty(t, fake.pvcs[0].GetOwnerReferences())
	require.Len(t, fake.pvcs[1].GetOwnerReferences(), 1, "owners other than the capsule must stay")
	assert.Equal(t, "other-owner", fake.pvcs[1].GetOwnerReferences()[0].Name)
	assert.Len(t, fake.pvcs[2].GetOwnerReferences(), 1, "another node's PVC must stay owned by its capsule")
}

// emptyDir and hostPath nodes have no PVCs.
func TestReleaseConsensusPVCs_NoPVCs(t *testing.T) {
	fake := &fakeCapsuleClient{}

	rpt := runUninstallStep(t, ReleaseConsensusPVCs(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, "0", rpt.Metadata["releasedCount"])
}

// releaseErrorClient fails every RemoveOwnerReference, like an RBAC denial on PVC update.
type releaseErrorClient struct {
	*fakeCapsuleClient
}

func (r *releaseErrorClient) RemoveOwnerReference(_ context.Context, _ kube.ResourceKind, _, name, _, _ string) (bool, error) {
	return false, errorx.ExternalError.New("forbidden: cannot update pvc %s", name)
}

// A failed release must stop the workflow before the capsule delete, so it fails the step.
func TestReleaseConsensusPVCs_ReleaseErrorFails(t *testing.T) {
	capsule0 := models.ConsensusCapsuleName("orbit", 0)
	kc := &releaseErrorClient{&fakeCapsuleClient{pvcs: []unstructured.Unstructured{pvcOwnedBy(capsule0+"-saved", capsule0)}}}

	rpt := runUninstallStep(t, ReleaseConsensusPVCs(uninstallInputs(), func(context.Context) (CapsuleKubeClient, error) { return kc, nil }))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "failed to release PVC")
}

func TestReleaseConsensusPVCs_ListErrorFails(t *testing.T) {
	kc := &listErrorClient{&fakeCapsuleClient{}}

	rpt := runUninstallStep(t, ReleaseConsensusPVCs(uninstallInputs(), func(context.Context) (CapsuleKubeClient, error) { return kc, nil }))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "failed to list PVCs")
}

// --- DeleteConsensusCapsule ---

func TestDeleteConsensusCapsule_Deletes(t *testing.T) {
	capsuleName := models.ConsensusCapsuleName("orbit", 0)
	fake := &fakeCapsuleClient{existing: map[string]string{capsuleName: "deployed"}}

	rpt := runUninstallStep(t, DeleteConsensusCapsule(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, []string{capsuleName}, fake.deleted)
}

func TestDeleteConsensusCapsule_AlreadyAbsent(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}

	rpt := runUninstallStep(t, DeleteConsensusCapsule(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, "an absent capsule must not fail uninstall")
	assert.Empty(t, fake.deleted)
}

func TestDeleteConsensusCapsule_TimesOutWhenCapsuleStays(t *testing.T) {
	origInterval, origTimeout := capsuleDeletePollInterval, capsuleDeleteTimeout
	capsuleDeletePollInterval, capsuleDeleteTimeout = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { capsuleDeletePollInterval, capsuleDeleteTimeout = origInterval, origTimeout })

	capsuleName := models.ConsensusCapsuleName("orbit", 0)
	stuck := &stuckDeleteClient{&fakeCapsuleClient{existing: map[string]string{capsuleName: "deployed"}}}
	provider := func(context.Context) (CapsuleKubeClient, error) { return stuck, nil }

	rpt := runUninstallStep(t, DeleteConsensusCapsule(uninstallInputs(), provider))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "still exists")
}

// --- DeleteConsensusConfigCRs ---

func allConfigCRNames(nodeId int64) map[string]string {
	scope := models.ConsensusNodeScope(nodeId)
	names := map[string]string{}
	for _, c := range consensusConfigCRKinds {
		names[models.ConsensusConfigCRName(scope, c.key)] = "deployed"
	}
	return names
}

func TestDeleteConsensusConfigCRs_DeletesAll(t *testing.T) {
	fake := &fakeCapsuleClient{existing: allConfigCRNames(0)}

	rpt := runUninstallStep(t, DeleteConsensusConfigCRs(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Len(t, fake.deleted, 11)
	assert.Equal(t, "11", rpt.Metadata["deletedCount"])
}

func TestDeleteConsensusConfigCRs_SkipsAbsent(t *testing.T) {
	existing := allConfigCRNames(0)
	delete(existing, models.ConsensusConfigCRName("node0", models.ConfigKeyLog4j2))
	fake := &fakeCapsuleClient{existing: existing}

	rpt := runUninstallStep(t, DeleteConsensusConfigCRs(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, "10", rpt.Metadata["deletedCount"])
}

func TestDeleteConsensusConfigCRs_KeepsOtherNodes(t *testing.T) {
	existing := allConfigCRNames(0)
	for name := range allConfigCRNames(1) {
		existing[name] = "deployed"
	}
	fake := &fakeCapsuleClient{existing: existing}

	rpt := runUninstallStep(t, DeleteConsensusConfigCRs(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	for name := range allConfigCRNames(1) {
		assert.Contains(t, fake.existing, name, "node1 config CRs must stay")
	}
}

// The delete list must name the same kind and CR as EnsureConfigCRs, or uninstall leaves CRs behind.
func TestConsensusConfigCRKinds_MatchEnsureConfigCRs(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runEnsureConfigCRs(t, consensusInputsWithConfigs("v1", models.ConfigSourceEmbedded), false, fake)
	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)

	applied := map[string]string{}
	for _, obj := range fake.appliedObjs {
		name := obj.(interface{ GetName() string }).GetName()
		applied[name] = obj.GetObjectKind().GroupVersionKind().Kind
	}

	want := map[string]string{}
	for _, c := range consensusConfigCRKinds {
		want[models.ConsensusConfigCRName("node0", c.key)] = c.kind
	}
	assert.Equal(t, want, applied)
}

// --- DeleteOrbitIfLast ---

func TestDeleteOrbitIfLast_KeepsOrbitWhenOtherNodesRemain(t *testing.T) {
	fake := &fakeCapsuleClient{
		existing:   map[string]string{"orbit": "deployed"},
		listResult: capsuleList(map[string]string{models.ConsensusCapsuleName("orbit", 1): "Active"}),
	}

	rpt := runUninstallStep(t, DeleteOrbitIfLast(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Empty(t, fake.deleted)
	assert.Equal(t, "false", rpt.Metadata[OrbitDeletedByThisStep])
}

// The uninstalled capsule can still be listed while it terminates; it must not count.
func TestDeleteOrbitIfLast_DeletesWhenOnlyThisNodeListed(t *testing.T) {
	fake := &fakeCapsuleClient{
		existing:   map[string]string{"orbit": "deployed"},
		listResult: capsuleList(map[string]string{models.ConsensusCapsuleName("orbit", 0): "Active"}),
	}

	rpt := runUninstallStep(t, DeleteOrbitIfLast(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, []string{"orbit"}, fake.deleted)
	assert.Equal(t, "true", rpt.Metadata[OrbitDeletedByThisStep])
}

func TestDeleteOrbitIfLast_TimesOutWhenOrbitStays(t *testing.T) {
	origInterval, origTimeout := capsuleDeletePollInterval, capsuleDeleteTimeout
	capsuleDeletePollInterval, capsuleDeleteTimeout = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { capsuleDeletePollInterval, capsuleDeleteTimeout = origInterval, origTimeout })

	stuck := &stuckDeleteClient{&fakeCapsuleClient{existing: map[string]string{"orbit": "deployed"}}}
	provider := func(context.Context) (CapsuleKubeClient, error) { return stuck, nil }

	rpt := runUninstallStep(t, DeleteOrbitIfLast(uninstallInputs(), provider))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "still exists")
}

func TestDeleteOrbitIfLast_OrbitAlreadyAbsent(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}

	rpt := runUninstallStep(t, DeleteOrbitIfLast(uninstallInputs(), fake.provider()))

	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, "false", rpt.Metadata[OrbitDeletedByThisStep])
}

// listErrorClient fails every List, like an API outage during the Orbit check.
type listErrorClient struct {
	*fakeCapsuleClient
}

func (l *listErrorClient) List(_ context.Context, _ kube.ResourceKind, _ string, _ kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	return nil, errorx.ExternalError.New("connection reset")
}

// deleteErrorClient fails every DeleteResource with a non-NotFound error, like an RBAC denial.
type deleteErrorClient struct {
	*fakeCapsuleClient
}

func (d *deleteErrorClient) DeleteResource(_ context.Context, _, kind, _, _ string) (bool, error) {
	return false, errorx.ExternalError.New("forbidden: cannot delete %s", kind)
}

// A failed list must not be read as "no nodes left"; the shared Orbit must stay.
func TestDeleteOrbitIfLast_ListErrorKeepsOrbit(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{"orbit": "deployed"}}
	kc := &listErrorClient{fake}

	rpt := runUninstallStep(t, DeleteOrbitIfLast(uninstallInputs(), func(context.Context) (CapsuleKubeClient, error) { return kc, nil }))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "failed to list ConsensusCapsules")
	assert.Empty(t, fake.deleted, "the Orbit must not be deleted when the list fails")
	assert.Contains(t, fake.existing, "orbit")
}

func TestDeleteOrbitIfLast_DeleteErrorFails(t *testing.T) {
	kc := &deleteErrorClient{&fakeCapsuleClient{existing: map[string]string{"orbit": "deployed"}}}

	rpt := runUninstallStep(t, DeleteOrbitIfLast(uninstallInputs(), func(context.Context) (CapsuleKubeClient, error) { return kc, nil }))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "failed to delete Orbit")
}

func TestDeleteConsensusCapsule_DeleteErrorFailsWithoutWaiting(t *testing.T) {
	capsuleName := models.ConsensusCapsuleName("orbit", 0)
	kc := &deleteErrorClient{&fakeCapsuleClient{existing: map[string]string{capsuleName: "deployed"}}}

	rpt := runUninstallStep(t, DeleteConsensusCapsule(uninstallInputs(), func(context.Context) (CapsuleKubeClient, error) { return kc, nil }))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "failed to delete ConsensusCapsule")
	assert.NotContains(t, rpt.Error.Error(), "still exists", "a delete error must not be reported as a wait timeout")
}

func TestDeleteConsensusConfigCRs_DeleteErrorFails(t *testing.T) {
	kc := &deleteErrorClient{&fakeCapsuleClient{existing: allConfigCRNames(0)}}

	rpt := runUninstallStep(t, DeleteConsensusConfigCRs(uninstallInputs(), func(context.Context) (CapsuleKubeClient, error) { return kc, nil }))

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "failed to delete")
}
