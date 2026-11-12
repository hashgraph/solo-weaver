// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakePVCClient is an in-memory ConsensusPVCClient. `expandable` maps a
// StorageClass name to its allowVolumeExpansion; `classNoField` are classes
// without that field; `calls` records the changes in order.
type fakePVCClient struct {
	pvcs          []unstructured.Unstructured
	capsuleExists bool
	expandable    map[string]bool
	classNoField  []string
	calls         []string
	waitErr       error
}

func (f *fakePVCClient) ResourceExists(_ context.Context, _, kind, _, name string) (bool, error) {
	if kind == "StorageClass" {
		_, ok := f.expandable[name]
		return ok || slices.Contains(f.classNoField, name), nil
	}
	return f.capsuleExists, nil
}

func (f *fakePVCClient) List(context.Context, kube.ResourceKind, string, kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{Items: f.pvcs}, nil
}

func (f *fakePVCClient) GetResourceNestedBool(_ context.Context, _, _, _, name string, _ ...string) (bool, bool, error) {
	v, ok := f.expandable[name]
	return v, ok, nil
}

func (f *fakePVCClient) ResizePVC(_ context.Context, _, name, size string) error {
	f.calls = append(f.calls, "resize:"+name+":"+size)
	return nil
}

func (f *fakePVCClient) DeletePVC(_ context.Context, _, name string) error {
	f.calls = append(f.calls, "delete:"+name)
	return nil
}

func (f *fakePVCClient) WaitForResource(_ context.Context, _ kube.ResourceKind, _, name string, _ kube.CheckFunc, _ time.Duration) error {
	f.calls = append(f.calls, "wait:"+name)
	return f.waitErr
}

func (f *fakePVCClient) provider() ConsensusPVCProvider {
	return func(context.Context) (ConsensusPVCClient, error) { return f, nil }
}

const savedPVC = "orbit-consensus-0-saved"

// pvcObj builds a Bound PVC for the given volume of node 0 in orbit "orbit".
func pvcObj(volume, size, storageClass string, accessModes ...string) unstructured.Unstructured {
	modes := make([]interface{}, 0, len(accessModes))
	for _, m := range accessModes {
		modes = append(modes, m)
	}
	spec := map[string]interface{}{
		"accessModes": modes,
		"resources":   map[string]interface{}{"requests": map[string]interface{}{"storage": size}},
	}
	if storageClass != "" {
		spec["storageClassName"] = storageClass
	}
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata":   map[string]interface{}{"name": models.ConsensusCapsuleName("orbit", 0) + "-" + volume, "namespace": "ns"},
		"spec":       spec,
		"status":     map[string]interface{}{"phase": "Bound"},
	}}
}

func savedPVCObj(size, storageClass string) unstructured.Unstructured {
	return pvcObj(models.ConsensusVolumeSaved, size, storageClass, "ReadWriteOnce")
}

// pvcInputs backs the saved volume with a PVC of the given settings; the other volumes are emptydir.
func pvcInputs(size, storageClass, accessMode string) models.ConsensusNodeInputs {
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(models.ConsensusVolumeSaved, models.ConsensusVolumeSpec{
		Type: models.VolumeBackingPVC, Size: size, StorageClass: storageClass, AccessMode: accessMode,
	})
	return models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0, Volumes: cfg}
}

// emptyDirInputs leaves every volume emptydir.
func emptyDirInputs() models.ConsensusNodeInputs {
	return models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0}
}

func runPrecheck(t *testing.T, fake *fakePVCClient, in models.ConsensusNodeInputs, force, nonInteractive bool) *automa.Report {
	t.Helper()
	step, err := PrecheckConsensusPVCs(in, force, nonInteractive, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func runPVCPrecheck(t *testing.T, in models.ConsensusNodeInputs, force, nonInteractive bool, pvcs ...unstructured.Unstructured) *automa.Report {
	t.Helper()
	return runPrecheck(t, &fakePVCClient{pvcs: pvcs}, in, force, nonInteractive)
}

func runReconcile(t *testing.T, fake *fakePVCClient, in models.ConsensusNodeInputs) *automa.Report {
	t.Helper()
	step, err := ReconcileConsensusPVCs(in, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func hintsOf(err error) []string {
	hints, _ := errx.Hints(err)
	return hints
}

// --- PrecheckConsensusPVCs: drift and stranded PVCs without flags ---

func TestPrecheckConsensusPVCs_FreshInstallPasses(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, false)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusPVCs_MatchingPVCPasses(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "fast-ssd", "ReadWriteOnce"), false, false,
		savedPVCObj("500Gi", "fast-ssd"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
	assert.Empty(t, rpt.Metadata[models.ReportMetaWarning], "a matching PVC gives no warning")
}

func TestPrecheckConsensusPVCs_EqualQuantitiesPass(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, false, savedPVCObj("1024Gi", "standard"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "1Ti and 1024Gi are the same size")
}

func TestPrecheckConsensusPVCs_EmptyStorageClassAcceptsClusterDefault(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "", ""), false, false, savedPVCObj("500Gi", "standard"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "an empty request means the cluster default class")
}

func TestPrecheckConsensusPVCs_SizeDriftStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusFailed, rpt.Status)

	msg := rpt.Error.Error()
	assert.Contains(t, msg, savedPVC)
	assert.Contains(t, msg, "size is 500Gi, requested 1Ti")

	hints := hintsOf(rpt.Error)
	assert.Contains(t, hints, "To keep the existing PVCs, install with their current values: --volume name=saved,size=500Gi")
	assert.Contains(t, hints, "Pass --expand-pvcs to grow "+savedPVC+" (the StorageClass must allow volume expansion; the data is kept)")
	assert.Contains(t, hints, "Pass --recreate-pvcs to delete these PVCs and create them with the requested settings (this deletes their data)")
	assert.Contains(t, hints, "Pass --force to reuse the existing PVCs as they are")
}

func TestPrecheckConsensusPVCs_ShrinkHasNoExpandHint(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("100Gi", "", ""), false, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	for _, h := range hintsOf(rpt.Error) {
		assert.NotContains(t, h, "--expand-pvcs", "a PVC cannot shrink")
	}
}

func TestPrecheckConsensusPVCs_StorageClassDriftStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "fast-ssd", ""), false, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "storageClass is standard, requested fast-ssd")
	assert.Contains(t, hintsOf(rpt.Error), "To keep the existing PVCs, install with their current values: --volume name=saved,storageClass=standard")
}

func TestPrecheckConsensusPVCs_AccessModeDriftStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "", "ReadWriteOncePod"), false, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "accessMode is ReadWriteOnce, requested ReadWriteOncePod")
}

func TestPrecheckConsensusPVCs_DriftWithForcePasses(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), true, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	// The TUI shows only report warnings on screen, not logs.
	assert.Equal(t, "PVC "+savedPVC+" is reused as it is (size is 500Gi, requested 1Ti); the requested settings have no effect",
		rpt.Metadata[models.ReportMetaWarning])
}

func TestPrecheckConsensusPVCs_DriftNonInteractiveStillStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, true, savedPVCObj("500Gi", "standard"))
	assert.Equal(t, automa.StatusFailed, rpt.Status, "only --force reuses a PVC that differs from the request")
}

func TestPrecheckConsensusPVCs_StrandedPVCStops(t *testing.T) {
	rpt := runPVCPrecheck(t, emptyDirInputs(), false, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusFailed, rpt.Status)

	assert.Contains(t, rpt.Error.Error(), "saved is emptydir, but PVC "+savedPVC+" exists")
	hints := hintsOf(rpt.Error)
	assert.Contains(t, hints, "To keep the data, back the volumes with their PVCs again: --volume name=saved,type=pvc,size=500Gi,storageClass=standard,accessMode=ReadWriteOnce")
	assert.Contains(t, hints, "Pass --recreate-pvcs to delete these PVCs (this deletes their data)")
	assert.Contains(t, hints, "Pass --force or --non-interactive to install without this data (the PVCs are kept)")
}

func TestPrecheckConsensusPVCs_StrandedHostPathStops(t *testing.T) {
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(models.ConsensusVolumeSaved, models.ConsensusVolumeSpec{Type: models.VolumeBackingHostPath})
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0, Volumes: cfg}

	rpt := runPVCPrecheck(t, in, false, false, savedPVCObj("500Gi", "standard"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "saved is hostpath")
}

func TestPrecheckConsensusPVCs_StrandedWithForceOrNonInteractivePasses(t *testing.T) {
	pvc := savedPVCObj("500Gi", "standard")
	want := "PVC " + savedPVC + " has data but is not mounted (saved is emptydir); the PVC is kept"
	for name, flags := range map[string][2]bool{"--force": {true, false}, "--non-interactive": {false, true}} {
		rpt := runPVCPrecheck(t, emptyDirInputs(), flags[0], flags[1], pvc)
		require.Equal(t, automa.StatusSuccess, rpt.Status, name)
		assert.Equal(t, want, rpt.Metadata[models.ReportMetaWarning], name)
	}
}

func TestPrecheckConsensusPVCs_IgnoresUnmanagedPVCs(t *testing.T) {
	other := savedPVCObj("500Gi", "standard")
	other.SetName(models.ConsensusCapsuleName("orbit", 1) + "-saved") // another node

	rpt := runPVCPrecheck(t, emptyDirInputs(), false, false,
		pvcObj("uc-data", "10Gi", "standard", "ReadWriteOnce"), other)
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "uc-data and other nodes' PVCs are not checked")
}

// --- planPVCs: which flag takes which PVC ---

func TestPlanPVCs_Precedence(t *testing.T) {
	growOnly := pvcDrift{volume: "saved", grow: "1Ti", mismatches: []pvcMismatch{{field: "size"}}}
	shrink := pvcDrift{volume: "state", mismatches: []pvcMismatch{{field: "size"}}}
	growAndClass := pvcDrift{volume: "logs", grow: "10Gi", mismatches: []pvcMismatch{{field: "size"}, {field: "storageClass"}}}
	stranded := []pvcStranded{{volume: "stats"}}
	drifts := []pvcDrift{growOnly, shrink, growAndClass}

	none := planPVCs(drifts, stranded, false, false)
	assert.Len(t, none.drifts, 3)
	assert.Len(t, none.stranded, 1)
	assert.False(t, none.hasActions())

	expand := planPVCs(drifts, stranded, true, false)
	require.Len(t, expand.expand, 1, "--expand-pvcs takes only the grow-only drift")
	assert.Equal(t, "saved", expand.expand[0].volume)
	assert.Len(t, expand.drifts, 2)
	assert.Len(t, expand.stranded, 1)
	assert.Empty(t, expand.remove)

	recreate := planPVCs(drifts, stranded, false, true)
	assert.Empty(t, recreate.expand)
	assert.Len(t, recreate.remove, 4, "--recreate-pvcs takes every drift and stranded PVC")
	assert.Empty(t, recreate.drifts)
	assert.Empty(t, recreate.stranded)

	both := planPVCs(drifts, stranded, true, true)
	require.Len(t, both.expand, 1, "--expand-pvcs wins for a grow-only drift")
	assert.Equal(t, "saved", both.expand[0].volume)
	assert.Len(t, both.remove, 3)
	for _, r := range both.remove {
		assert.Equal(t, r.volume != "stats", r.recreated, "only a pvc-backed volume is created again")
	}
}

// --- --expand-pvcs ---

func expandInputs() models.ConsensusNodeInputs {
	in := pvcInputs("1Ti", "", "")
	in.ExpandPVCs = true
	return in
}

func TestPrecheckConsensusPVCs_ExpandPasses(t *testing.T) {
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, expandable: map[string]bool{"standard": true}}
	rpt := runPrecheck(t, fake, expandInputs(), false, false)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
	assert.Empty(t, fake.calls, "the precheck changes nothing")
}

func TestPrecheckConsensusPVCs_ExpandBlocked(t *testing.T) {
	notBound := savedPVCObj("500Gi", "standard")
	_ = unstructured.SetNestedField(notBound.Object, "Pending", "status", "phase")

	cases := []struct {
		name         string
		pvc          unstructured.Unstructured
		expandable   map[string]bool
		classNoField []string
		want         string
	}{
		{"class disallows expansion", savedPVCObj("500Gi", "standard"), map[string]bool{"standard": false}, nil, "StorageClass standard does not allow volume expansion"},
		{"class without the field", savedPVCObj("500Gi", "standard"), nil, []string{"standard"}, "StorageClass standard does not allow volume expansion"},
		{"class not found", savedPVCObj("500Gi", "standard"), nil, nil, "StorageClass standard not found"},
		{"no class", savedPVCObj("500Gi", ""), nil, nil, "the PVC has no StorageClass"},
		{"not bound", notBound, map[string]bool{"standard": true}, nil, "the PVC is Pending, not Bound"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Even with --force: a requested expand that cannot run must not be skipped.
			fake := &fakePVCClient{pvcs: []unstructured.Unstructured{tc.pvc}, expandable: tc.expandable, classNoField: tc.classNoField}
			rpt := runPrecheck(t, fake, expandInputs(), true, false)
			require.Equal(t, automa.StatusFailed, rpt.Status)
			assert.Contains(t, rpt.Error.Error(), tc.want)
			assert.Contains(t, hintsOf(rpt.Error), "To keep the PVCs, install with their current size: --volume name=saved,size=500Gi")
		})
	}
}

func TestPrecheckConsensusPVCs_ExpandBlockedWithBothFlagsDoesNotDelete(t *testing.T) {
	in := expandInputs()
	in.RecreatePVCs = true
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, expandable: map[string]bool{"standard": false}}
	rpt := runPrecheck(t, fake, in, false, false)
	require.Equal(t, automa.StatusFailed, rpt.Status, "a failed expand never falls back to a delete")
	assert.Contains(t, hintsOf(rpt.Error), "Run install with --recreate-pvcs and without --expand-pvcs to delete these PVCs and create them again (this deletes their data)")
	assert.Empty(t, fake.calls)
}

func TestPrecheckConsensusPVCs_ExpandDoesNotCoverOtherDrift(t *testing.T) {
	in := pvcInputs("1Ti", "fast-ssd", "")
	in.ExpandPVCs = true
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, expandable: map[string]bool{"standard": true}}
	rpt := runPrecheck(t, fake, in, false, false)
	require.Equal(t, automa.StatusFailed, rpt.Status, "a class change cannot be expanded")
	assert.Contains(t, rpt.Error.Error(), "storageClass is standard, requested fast-ssd")
}

func TestReconcileConsensusPVCs_ExpandResizesWithoutWait(t *testing.T) {
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, expandable: map[string]bool{"standard": true}}
	rpt := runReconcile(t, fake, expandInputs())
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	assert.Equal(t, []string{"resize:" + savedPVC + ":1Ti"}, fake.calls)
}

func TestReconcileConsensusPVCs_ExpandRevalidates(t *testing.T) {
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, expandable: map[string]bool{"standard": false}}
	rpt := runReconcile(t, fake, expandInputs())
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Empty(t, fake.calls)
}

// --- --recreate-pvcs ---

func TestPrecheckConsensusPVCs_RecreatePasses(t *testing.T) {
	in := pvcInputs("500Gi", "fast-ssd", "")
	in.RecreatePVCs = true
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}}
	rpt := runPrecheck(t, fake, in, false, false)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
	assert.Empty(t, fake.calls, "the precheck changes nothing")
}

func TestPrecheckConsensusPVCs_RecreateStopsWhenCapsuleExists(t *testing.T) {
	in := emptyDirInputs()
	in.RecreatePVCs = true
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, capsuleExists: true}
	rpt := runPrecheck(t, fake, in, true, false)
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "while ConsensusCapsule")
	assert.Contains(t, hintsOf(rpt.Error), "Uninstall the node first: sudo solo-provisioner consensus node uninstall --experimental --namespace ns --node-id 0")
}

func TestReconcileConsensusPVCs_RecreateDeletesOnlyPlannedPVCs(t *testing.T) {
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(models.ConsensusVolumeSaved, models.ConsensusVolumeSpec{Type: models.VolumeBackingPVC, Size: "1Ti"})
	cfg.SetVolume(models.ConsensusVolumeState, models.ConsensusVolumeSpec{Type: models.VolumeBackingPVC, Size: "100Gi"})
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0, Volumes: cfg, RecreatePVCs: true}

	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{
		savedPVCObj("500Gi", "standard"),                                          // differs: deleted
		pvcObj(models.ConsensusVolumeState, "100Gi", "standard", "ReadWriteOnce"), // matches: kept
		pvcObj(models.ConsensusVolumeLogs, "5Gi", "standard", "ReadWriteOnce"),    // stranded: deleted
	}}
	rpt := runReconcile(t, fake, in)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	assert.Equal(t, []string{
		"delete:" + savedPVC, "wait:" + savedPVC,
		"delete:orbit-consensus-0-logs", "wait:orbit-consensus-0-logs",
	}, fake.calls)
}

func TestReconcileConsensusPVCs_DeleteWaitTimeoutFails(t *testing.T) {
	in := emptyDirInputs()
	in.RecreatePVCs = true
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, waitErr: errors.New("timed out")}
	rpt := runReconcile(t, fake, in)
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, hintsOf(rpt.Error), "See what still uses the PVC (Used By): kubectl -n ns describe pvc "+savedPVC)
}

func TestReconcileConsensusPVCs_RecreateStopsWhenCapsuleExists(t *testing.T) {
	in := emptyDirInputs()
	in.RecreatePVCs = true
	fake := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}, capsuleExists: true}
	rpt := runReconcile(t, fake, in)
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Empty(t, fake.calls)
}
