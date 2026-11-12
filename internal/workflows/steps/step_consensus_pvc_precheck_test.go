// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// pvcObj builds a live PVC for the given volume of node 0 in orbit "orbit".
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
	}}
}

// pvcInputs backs the saved volume with a PVC of the given settings; the other volumes are emptydir.
func pvcInputs(size, storageClass, accessMode string) models.ConsensusNodeInputs {
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(models.ConsensusVolumeSaved, models.ConsensusVolumeSpec{
		Type: models.VolumeBackingPVC, Size: size, StorageClass: storageClass, AccessMode: accessMode,
	})
	return models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0, Volumes: cfg}
}

func runPVCPrecheck(t *testing.T, in models.ConsensusNodeInputs, force, nonInteractive bool, pvcs ...unstructured.Unstructured) *automa.Report {
	t.Helper()
	fake := &fakeCapsuleClient{pvcs: pvcs}
	step, err := PrecheckConsensusPVCs(in, force, nonInteractive, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestPrecheckConsensusPVCs_FreshInstallPasses(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, false)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusPVCs_MatchingPVCPasses(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "fast-ssd", "ReadWriteOnce"), false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "fast-ssd", "ReadWriteOnce"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusPVCs_EqualQuantitiesPass(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, false,
		pvcObj(models.ConsensusVolumeSaved, "1024Gi", "standard", "ReadWriteOnce"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "1Ti and 1024Gi are the same size")
}

func TestPrecheckConsensusPVCs_EmptyStorageClassAcceptsClusterDefault(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "", ""), false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "an empty request means the cluster default class")
}

func TestPrecheckConsensusPVCs_SizeDriftStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	require.Equal(t, automa.StatusFailed, rpt.Status)

	msg := rpt.Error.Error()
	assert.Contains(t, msg, "orbit-consensus-0-saved")
	assert.Contains(t, msg, "size is 500Gi, requested 1Ti")

	hints := hintsOf(rpt.Error)
	assert.Contains(t, hints, "To keep the existing PVCs, install with their current values: --volume name=saved,size=500Gi")
	assert.Contains(t, hints, `To grow orbit-consensus-0-saved (only if its StorageClass allows volume expansion): kubectl -n ns patch pvc orbit-consensus-0-saved -p '{"spec":{"resources":{"requests":{"storage":"1Ti"}}}}'`)
	assert.Contains(t, hints, "Pass --force to reuse the existing PVCs as they are")
}

func TestPrecheckConsensusPVCs_ShrinkHasNoGrowHint(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("100Gi", "", ""), false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	for _, h := range hintsOf(rpt.Error) {
		assert.NotContains(t, h, "patch pvc", "a PVC cannot shrink")
	}
}

func TestPrecheckConsensusPVCs_StorageClassDriftStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "fast-ssd", ""), false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "storageClass is standard, requested fast-ssd")
	assert.Contains(t, hintsOf(rpt.Error), "To keep the existing PVCs, install with their current values: --volume name=saved,storageClass=standard")
}

func TestPrecheckConsensusPVCs_AccessModeDriftStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("500Gi", "", "ReadWriteOncePod"), false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "accessMode is ReadWriteOnce, requested ReadWriteOncePod")
}

func TestPrecheckConsensusPVCs_DriftWithForcePasses(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), true, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusPVCs_DriftNonInteractiveStillStops(t *testing.T) {
	rpt := runPVCPrecheck(t, pvcInputs("1Ti", "", ""), false, true,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	assert.Equal(t, automa.StatusFailed, rpt.Status, "only --force reuses a PVC that differs from the request")
}

func TestPrecheckConsensusPVCs_StrandedPVCStops(t *testing.T) {
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0} // all emptydir
	rpt := runPVCPrecheck(t, in, false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	require.Equal(t, automa.StatusFailed, rpt.Status)

	assert.Contains(t, rpt.Error.Error(), "saved is emptydir, but PVC orbit-consensus-0-saved exists")
	hints := hintsOf(rpt.Error)
	assert.Contains(t, hints, "To keep the data, back the volumes with their PVCs again: --volume name=saved,type=pvc,size=500Gi,storageClass=standard,accessMode=ReadWriteOnce")
	assert.Contains(t, hints, "Pass --force or --non-interactive to install without this data (the PVCs are kept)")
}

func TestPrecheckConsensusPVCs_StrandedHostPathStops(t *testing.T) {
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(models.ConsensusVolumeSaved, models.ConsensusVolumeSpec{Type: models.VolumeBackingHostPath})
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0, Volumes: cfg}

	rpt := runPVCPrecheck(t, in, false, false,
		pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "saved is hostpath")
}

func TestPrecheckConsensusPVCs_StrandedWithForceOrNonInteractivePasses(t *testing.T) {
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0}
	pvc := pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce")

	assert.Equal(t, automa.StatusSuccess, runPVCPrecheck(t, in, true, false, pvc).Status, "--force")
	assert.Equal(t, automa.StatusSuccess, runPVCPrecheck(t, in, false, true, pvc).Status, "--non-interactive")
}

func TestPrecheckConsensusPVCs_IgnoresUnmanagedPVCs(t *testing.T) {
	in := models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0}
	other := pvcObj(models.ConsensusVolumeSaved, "500Gi", "standard", "ReadWriteOnce")
	other.SetName(models.ConsensusCapsuleName("orbit", 1) + "-saved") // another node

	rpt := runPVCPrecheck(t, in, false, false,
		pvcObj("uc-data", "10Gi", "standard", "ReadWriteOnce"), other)
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "uc-data and other nodes' PVCs are not checked")
}

func hintsOf(err error) []string {
	hints, _ := errx.Hints(err)
	return hints
}
