// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bnCapsuleInputs() models.BlockNodeInputs {
	return models.BlockNodeInputs{
		Namespace:    "block-node",
		Release:      "block-node",
		Chart:        "oci://ghcr.io/hiero-ledger/hiero-block-node",
		ChartName:    "block-node-server",
		ChartVersion: "0.44.2",
	}
}

func runPrecheckBlockNode(t *testing.T, fake *fakeCapsuleClient, force bool) *automa.Report {
	t.Helper()
	step, err := PrecheckBlockNode(bnCapsuleInputs(), force, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func staticValues(yamlDoc string) func(context.Context) ([]byte, error) {
	return func(context.Context) ([]byte, error) { return []byte(yamlDoc), nil }
}

func runCreateBlockNodeCapsule(t *testing.T, fake *fakeCapsuleClient, reuse bool, legacy func() (map[string]any, error)) *automa.Report {
	t.Helper()
	step, err := CreateBlockNodeCapsule(bnCapsuleInputs(), staticValues("replicaCount: 1\n"), reuse, legacy, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func mustNotReadLegacy(t *testing.T, why string) func() (map[string]any, error) {
	return func() (map[string]any, error) {
		t.Fatal(why)
		return nil, nil
	}
}

func appliedCapsuleValues(t *testing.T, fake *fakeCapsuleClient) string {
	t.Helper()
	require.Len(t, fake.appliedObjs, 1)
	capsule, ok := fake.appliedObjs[0].(*operatorv1alpha1.HelmCapsule)
	require.True(t, ok)
	require.NotNil(t, capsule.Spec.Values)
	return string(capsule.Spec.Values.Raw)
}

// --- PrecheckBlockNode (new CR-model install guard) ---

// No CR → install may proceed.
func TestPrecheckBlockNode_Absent(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runPrecheckBlockNode(t, fake, false)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

// CR exists, no --force → refuse.
func TestPrecheckBlockNode_CapsuleExistsNoForce(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{"block-node": "deployed"}}
	rpt := runPrecheckBlockNode(t, fake, false)
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "already installed")
	assert.Contains(t, rpt.Error.Error(), "--force")
}

// CR exists, --force → re-apply path proceeds.
func TestPrecheckBlockNode_CapsuleExistsWithForce(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{"block-node": "deployed"}}
	rpt := runPrecheckBlockNode(t, fake, true)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestCreateBlockNodeCapsule_AdoptsLegacyReleaseValues(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	legacy := func() (map[string]any, error) {
		return map[string]any{"replicaCount": 0, "operatorExtra": "kept"}, nil
	}

	rpt := runCreateBlockNodeCapsule(t, fake, true, legacy)

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.JSONEq(t, `{"replicaCount":1,"operatorExtra":"kept"}`, appliedCapsuleValues(t, fake))
}

func TestCreateBlockNodeCapsule_CapsuleValuesWinOverLegacy(t *testing.T) {
	fake := &fakeCapsuleClient{objects: map[string]map[string]interface{}{
		"block-node": {"spec": map[string]interface{}{"values": map[string]interface{}{"fromCapsule": "yes"}}},
	}}

	rpt := runCreateBlockNodeCapsule(t, fake, true, mustNotReadLegacy(t, "legacy release must not be read when the HelmCapsule exists"))

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.JSONEq(t, `{"replicaCount":1,"fromCapsule":"yes"}`, appliedCapsuleValues(t, fake))
}

func TestCreateBlockNodeCapsule_NoReuseIgnoresLegacyValues(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runCreateBlockNodeCapsule(t, fake, false, mustNotReadLegacy(t, "legacy release must not be read under --no-reuse-values"))

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.JSONEq(t, `{"replicaCount":1}`, appliedCapsuleValues(t, fake))
}

func TestCreateBlockNodeCapsule_LegacyReadErrorFails(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	legacy := func() (map[string]any, error) { return nil, errorx.IllegalState.New("helm boom") }

	rpt := runCreateBlockNodeCapsule(t, fake, true, legacy)

	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Empty(t, fake.applied)
}

func runWaitReady(t *testing.T, fake *fakeCapsuleClient) *automa.Report {
	t.Helper()
	prev := blockNodeCapsuleReadyPollInterval
	blockNodeCapsuleReadyPollInterval = time.Millisecond
	t.Cleanup(func() { blockNodeCapsuleReadyPollInterval = prev })
	ins := bnCapsuleInputs()
	ins.Timeout = 5 * time.Millisecond
	step, err := WaitForBlockNodeCapsuleReady(ins, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func capsuleStatus(phase string, generation, observed int64) *fakeCapsuleClient {
	return &fakeCapsuleClient{objects: map[string]map[string]interface{}{
		"block-node": {
			"metadata": map[string]interface{}{"generation": generation},
			"status":   map[string]interface{}{"phase": phase, "observedGeneration": observed},
		},
	}}
}

func TestWaitForBlockNodeCapsuleReady_SucceededCurrentGeneration(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseSucceeded), 2, 2))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestWaitForBlockNodeCapsuleReady_FailedCurrentGeneration(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseFailed), 2, 2))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "phase=Failed")
}

func TestWaitForBlockNodeCapsuleReady_RunningIsNotReady(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseRunning), 2, 2))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "timed out")
}

func TestWaitForBlockNodeCapsuleReady_StaleSucceededIsNotReady(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseSucceeded), 3, 2))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "timed out")
}

// Adopting a legacy release needs the Orbit in place before the capsule is applied.
func TestUpgradeBlockNode_EnsuresOrbitBeforeCapsule(t *testing.T) {
	stp, err := UpgradeBlockNode(bnCapsuleInputs()).Build()
	require.NoError(t, err)
	wf, ok := stp.(automa.Workflow)
	require.True(t, ok, "expected a Workflow, got %T", stp)

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	assert.Equal(t, []string{
		"ensure-hedera-owner",
		EnsureStorageMigrationsStepId,
		RecordBlockNodeServiceShapeStepId,
		EnsureBlockNodeOrbitStepId,
		CreateBlockNodeCapsuleStepId,
		WaitForBlockNodeCapsuleReadyStepId,
		HealCiliumServiceShapeStepId,
		VerifyBlockNodeReachableStepId,
	}, ids)
}

// splitBlockNodeChart derives the HelmCapsule repo + chart name from inputs.
func TestSplitBlockNodeChart(t *testing.T) {
	// Full OCI ref, no explicit ChartName: last segment is the chart name.
	repo, chart := splitBlockNodeChart(models.BlockNodeInputs{
		Chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
	})
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node", repo)
	assert.Equal(t, "block-node-server", chart)

	// Explicit ChartName is authoritative; Chart is the bare repo.
	repo, chart = splitBlockNodeChart(models.BlockNodeInputs{
		Chart:     "oci://ghcr.io/hiero-ledger/hiero-block-node",
		ChartName: "block-node-server",
	})
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node", repo)
	assert.Equal(t, "block-node-server", chart)
}

// buildBlockNodeHelmCapsule sets the required HelmCapsule fields from inputs.
func TestBuildBlockNodeHelmCapsule(t *testing.T) {
	capsule, err := buildBlockNodeHelmCapsule(bnCapsuleInputs(), []byte("service:\n  type: LoadBalancer\n"))
	require.NoError(t, err)

	assert.Equal(t, kube.SoloOperatorAPIVersion, capsule.APIVersion)
	assert.Equal(t, string(kube.KindHelmCapsule), capsule.Kind)
	assert.Equal(t, "block-node", capsule.Name)
	assert.Equal(t, "block-node", capsule.Namespace)
	require.NotNil(t, capsule.Spec.Component)
	assert.Equal(t, "block-node", string(*capsule.Spec.Component))
	require.NotNil(t, capsule.Spec.OrbitRef)
	assert.Equal(t, "block-node", capsule.Spec.OrbitRef.Name)
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node", capsule.Spec.Chart.Repo)
	assert.Equal(t, "block-node-server", capsule.Spec.Chart.Chart)
	assert.Equal(t, "0.44.2", capsule.Spec.Chart.Version)
	require.NotNil(t, capsule.Spec.Values)
	assert.JSONEq(t, `{"service":{"type":"LoadBalancer"}}`, string(capsule.Spec.Values.Raw))
}
