// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func storageClass(name, provisioner string, annotations map[string]string) unstructured.Unstructured {
	sc := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion":  "storage.k8s.io/v1",
		"kind":        "StorageClass",
		"provisioner": provisioner,
	}}
	sc.SetName(name)
	sc.SetAnnotations(annotations)
	return sc
}

func defaultStorageClass(name string) unstructured.Unstructured {
	return storageClass(name, "rancher.io/local-path", map[string]string{defaultStorageClassAnnotation: "true"})
}

func storageInputs(cfg models.ConsensusVolumeConfig) models.ConsensusNodeInputs {
	return models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "orbit", NodeId: 0, Volumes: cfg}
}

func pvcDefaultInputs(storageClass string) models.ConsensusNodeInputs {
	return storageInputs(models.ConsensusVolumeConfig{
		Defaults: models.ConsensusVolumeDefaults{Type: models.VolumeBackingPVC, StorageClass: storageClass},
	})
}

func reasonOf(err error) errx.Reason {
	r, _ := errx.ReasonOf(err)
	return r
}

func hintsOf(err error) []string {
	h, _ := errx.Hints(err)
	return h
}

func runStoragePrecheck(t *testing.T, in models.ConsensusNodeInputs, force bool, provider CapsuleKubeProvider) *automa.Report {
	t.Helper()
	step, err := PrecheckConsensusStorageClasses(in, force, provider).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestPrecheckConsensusStorageClasses_NoStorageClassFails(t *testing.T) {
	fake := &fakeCapsuleClient{}
	rpt := runStoragePrecheck(t, pvcDefaultInputs(""), false, fake.provider())

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, reasons.PreconditionNotMet, reasonOf(rpt.Error))
	assert.Contains(t, rpt.Error.Error(), "no default StorageClass")
	assert.NotEmpty(t, hintsOf(rpt.Error))
}

func TestPrecheckConsensusStorageClasses_DefaultExistsPasses(t *testing.T) {
	fake := &fakeCapsuleClient{storageClasses: []unstructured.Unstructured{defaultStorageClass("local-path")}}
	rpt := runStoragePrecheck(t, pvcDefaultInputs(""), false, fake.provider())
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusStorageClasses_BetaDefaultAnnotationPasses(t *testing.T) {
	fake := &fakeCapsuleClient{storageClasses: []unstructured.Unstructured{
		storageClass("legacy", "example.com/csi", map[string]string{betaDefaultStorageClassAnnotation: "true"}),
	}}
	rpt := runStoragePrecheck(t, pvcDefaultInputs(""), false, fake.provider())
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusStorageClasses_NonDefaultOnlyFails(t *testing.T) {
	fake := &fakeCapsuleClient{storageClasses: []unstructured.Unstructured{
		storageClass("fast-ssd", "example.com/csi", nil),
		storageClass("marked-false", "example.com/csi", map[string]string{defaultStorageClassAnnotation: "false"}),
	}}
	rpt := runStoragePrecheck(t, pvcDefaultInputs(""), false, fake.provider())
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "no default StorageClass")
}

func TestPrecheckConsensusStorageClasses_NamedMissingFails(t *testing.T) {
	fake := &fakeCapsuleClient{storageClasses: []unstructured.Unstructured{defaultStorageClass("local-path")}}
	rpt := runStoragePrecheck(t, pvcDefaultInputs("does-not-exist"), false, fake.provider())

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, reasons.PreconditionNotMet, reasonOf(rpt.Error))
	assert.Contains(t, rpt.Error.Error(), `"does-not-exist"`)
}

func TestPrecheckConsensusStorageClasses_NamedExistsPasses(t *testing.T) {
	fake := &fakeCapsuleClient{storageClasses: []unstructured.Unstructured{storageClass("fast-ssd", "example.com/csi", nil)}}
	rpt := runStoragePrecheck(t, pvcDefaultInputs("fast-ssd"), false, fake.provider())
	assert.Equal(t, automa.StatusSuccess, rpt.Status, "a named StorageClass needs no default")
}

func TestPrecheckConsensusStorageClasses_AllMissingInOneError(t *testing.T) {
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(models.ConsensusVolumeSaved, models.ConsensusVolumeSpec{Type: models.VolumeBackingPVC, StorageClass: "sc-a"})
	cfg.SetVolume(models.ConsensusVolumeState, models.ConsensusVolumeSpec{Type: models.VolumeBackingPVC, StorageClass: "sc-b"})
	cfg.SetVolume(models.ConsensusVolumeBlocks, models.ConsensusVolumeSpec{Type: models.VolumeBackingPVC})
	fake := &fakeCapsuleClient{}

	rpt := runStoragePrecheck(t, storageInputs(cfg), false, fake.provider())
	require.Equal(t, automa.StatusFailed, rpt.Status)
	msg := rpt.Error.Error()
	assert.Contains(t, msg, `"sc-a"`)
	assert.Contains(t, msg, `"sc-b"`)
	assert.Contains(t, msg, "no default StorageClass")
	assert.Contains(t, msg, models.ConsensusVolumeBlocks)
}

func TestPrecheckConsensusStorageClasses_SingleVolumeFlagTriggersCheck(t *testing.T) {
	name, spec, err := models.ParseVolumeArg("name=saved,type=pvc")
	require.NoError(t, err)
	cfg := models.ConsensusVolumeConfig{}
	cfg.SetVolume(name, spec)
	in := storageInputs(cfg)

	require.True(t, ConsensusHasPVCVolumes(in))
	fake := &fakeCapsuleClient{}
	rpt := runStoragePrecheck(t, in, false, fake.provider())
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, 1, fake.scListCalls)
}

func TestPrecheckConsensusStorageClasses_VolumesFileTriggersCheck(t *testing.T) {
	for _, tc := range []struct{ name, yaml string }{
		{"defaults", "defaults:\n  type: pvc\n"},
		{"one volume", "volumes:\n  saved:\n    type: pvc\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := models.LoadVolumeConfigYAML([]byte(tc.yaml))
			require.NoError(t, err)
			in := storageInputs(cfg)

			require.True(t, ConsensusHasPVCVolumes(in))
			rpt := runStoragePrecheck(t, in, false, (&fakeCapsuleClient{}).provider())
			assert.Equal(t, automa.StatusFailed, rpt.Status)
		})
	}
}

func TestPrecheckConsensusStorageClasses_NoPVCSkipsKubeAPI(t *testing.T) {
	for _, backing := range []string{"", models.VolumeBackingEmptyDir, models.VolumeBackingHostPath} {
		in := storageInputs(models.ConsensusVolumeConfig{Defaults: models.ConsensusVolumeDefaults{Type: backing}})
		assert.False(t, ConsensusHasPVCVolumes(in), "backing %q", backing)

		called := false
		provider := func(context.Context) (CapsuleKubeClient, error) {
			called = true
			return nil, errors.New("must not be called")
		}
		rpt := runStoragePrecheck(t, in, false, provider)
		assert.Equal(t, automa.StatusSuccess, rpt.Status)
		assert.False(t, called, "backing %q must not resolve a kube client", backing)
	}
}

func TestPrecheckConsensusStorageClasses_ForceWarnsAndContinues(t *testing.T) {
	fake := &fakeCapsuleClient{}
	rpt := runStoragePrecheck(t, pvcDefaultInputs("does-not-exist"), true, fake.provider())
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusStorageClasses_ListErrorFails(t *testing.T) {
	fake := &fakeCapsuleClient{scListErr: errors.New("forbidden")}
	rpt := runStoragePrecheck(t, pvcDefaultInputs(""), false, fake.provider())

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, reasons.K8sAPIFailed, reasonOf(rpt.Error))
}

func TestPrecheckConsensusStorageClasses_ProviderErrorFails(t *testing.T) {
	provider := func(context.Context) (CapsuleKubeClient, error) { return nil, errors.New("no kubeconfig") }
	rpt := runStoragePrecheck(t, pvcDefaultInputs(""), false, provider)

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, reasons.PreconditionNotMet, reasonOf(rpt.Error))
}

func TestFindStorageClassGaps_NoProvisionerWarnsOnlyForUsedClasses(t *testing.T) {
	needs := storageClassNeeds{
		named:        map[string][]string{"static": {models.ConsensusVolumeSaved}},
		defaultUsers: []string{models.ConsensusVolumeState},
	}
	classes := []unstructured.Unstructured{
		storageClass("static", noProvisionerStorageClass, nil),
		storageClass("default-static", noProvisionerStorageClass, map[string]string{defaultStorageClassAnnotation: "true"}),
		storageClass("unused", noProvisionerStorageClass, nil),
	}

	gaps := findStorageClassGaps(needs, classes)
	assert.True(t, gaps.ok(), "no-provisioner is a warning, not a failure")
	assert.Equal(t, []string{"default-static", "static"}, gaps.noProvisioner)
}
