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
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/runtime"
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

func runCreateBlockNodeCapsule(t *testing.T, fake *fakeCapsuleClient, reuse bool, legacy func() (*release.Release, error), restore *blockNodeCapsuleRestore) *automa.Report {
	t.Helper()
	step, err := CreateBlockNodeCapsule(bnCapsuleInputs(), staticValues("replicaCount: 1\n"), reuse, legacy, restore, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func legacyRelease(version string, config map[string]any) func() (*release.Release, error) {
	return func() (*release.Release, error) {
		return &release.Release{Name: "block-node", Chart: &chart.Chart{Metadata: &chart.Metadata{Version: version}}, Config: config}, nil
	}
}

func mustNotReadLegacy(t *testing.T, why string) func() (*release.Release, error) {
	return func() (*release.Release, error) {
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
	restore := &blockNodeCapsuleRestore{}

	rpt := runCreateBlockNodeCapsule(t, fake, true, legacyRelease("0.40.0", map[string]any{"replicaCount": 0, "operatorExtra": "kept"}), restore)

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.JSONEq(t, `{"replicaCount":1,"operatorExtra":"kept"}`, appliedCapsuleValues(t, fake))
	require.True(t, restore.captured)
	require.NotNil(t, restore.spec)
	assert.Equal(t, "0.40.0", restore.spec.Chart.Version, "restore must target the legacy chart version")
	assert.JSONEq(t, `{"replicaCount":0,"operatorExtra":"kept"}`, string(restore.spec.Values.Raw))
}

func TestCreateBlockNodeCapsule_CapsuleValuesWinOverLegacy(t *testing.T) {
	fake := &fakeCapsuleClient{objects: map[string]map[string]interface{}{
		"block-node": {"spec": map[string]interface{}{"values": map[string]interface{}{"fromCapsule": "yes"}}},
	}}

	restore := &blockNodeCapsuleRestore{}

	rpt := runCreateBlockNodeCapsule(t, fake, true, mustNotReadLegacy(t, "legacy release must not be read when the HelmCapsule exists"), restore)

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.JSONEq(t, `{"replicaCount":1,"fromCapsule":"yes"}`, appliedCapsuleValues(t, fake))
	require.NotNil(t, restore.spec)
	assert.JSONEq(t, `{"fromCapsule":"yes"}`, string(restore.spec.Values.Raw))
}

func TestCreateBlockNodeCapsule_NoReuseIgnoresLegacyValues(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	restore := &blockNodeCapsuleRestore{}

	rpt := runCreateBlockNodeCapsule(t, fake, false, legacyRelease("0.40.0", map[string]any{"operatorExtra": "kept"}), restore)

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.JSONEq(t, `{"replicaCount":1}`, appliedCapsuleValues(t, fake))
	require.NotNil(t, restore.spec, "restore still needs the legacy spec under --no-reuse-values")
	assert.JSONEq(t, `{"operatorExtra":"kept"}`, string(restore.spec.Values.Raw))
}

func TestCreateBlockNodeCapsule_NothingDeployedRestoresToNil(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	restore := &blockNodeCapsuleRestore{}
	none := func() (*release.Release, error) { return nil, nil }

	rpt := runCreateBlockNodeCapsule(t, fake, true, none, restore)

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	assert.True(t, restore.captured)
	assert.Nil(t, restore.spec)
}

func TestCreateBlockNodeCapsule_LegacyReadErrorFails(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	restore := &blockNodeCapsuleRestore{}
	legacy := func() (*release.Release, error) { return nil, errorx.IllegalState.New("helm boom") }

	rpt := runCreateBlockNodeCapsule(t, fake, true, legacy, restore)

	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Empty(t, fake.applied)
	assert.False(t, restore.captured)
}

func runWaitReady(t *testing.T, fake *fakeCapsuleClient) *automa.Report {
	t.Helper()
	return runWaitReadyWith(t, fake.provider(), nil)
}

func runWaitReadyWith(t *testing.T, provider CapsuleKubeProvider, restore *blockNodeCapsuleRestore) *automa.Report {
	t.Helper()
	prev := blockNodeCapsuleReadyPollInterval
	blockNodeCapsuleReadyPollInterval = time.Millisecond
	t.Cleanup(func() { blockNodeCapsuleReadyPollInterval = prev })
	ins := bnCapsuleInputs()
	ins.Timeout = 5 * time.Millisecond
	step, err := WaitForBlockNodeCapsuleReady(ins, restore, provider).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func capsuleStatus(phase, ready string, generation, observed int64) *fakeCapsuleClient {
	return &fakeCapsuleClient{objects: map[string]map[string]interface{}{
		"block-node": {
			"metadata": map[string]interface{}{"generation": generation},
			"status": map[string]interface{}{
				"phase":              phase,
				"observedGeneration": observed,
				"conditions":         []interface{}{map[string]interface{}{"type": "Ready", "status": ready}},
			},
		},
	}}
}

func TestWaitForBlockNodeCapsuleReady_SucceededCurrentGeneration(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseSucceeded), "True", 2, 2))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestWaitForBlockNodeCapsuleReady_FailedCurrentGeneration(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseFailed), "False", 2, 2))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "phase=Failed")
}

func TestWaitForBlockNodeCapsuleReady_RunningIsNotReady(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseRunning), "False", 2, 2))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "timed out")
}

func TestWaitForBlockNodeCapsuleReady_StaleSucceededIsNotReady(t *testing.T) {
	rpt := runWaitReady(t, capsuleStatus(string(operatorv1alpha1.PhaseSucceeded), "True", 3, 2))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "timed out")
}

// readyOnApply turns the capsule Ready once a spec is applied, as the operator would.
type readyOnApply struct{ *fakeCapsuleClient }

func (r readyOnApply) ApplyTyped(ctx context.Context, obj runtime.Object) error {
	r.objects["block-node"] = capsuleStatus(string(operatorv1alpha1.PhaseSucceeded), "True", 3, 3).objects["block-node"]
	return r.fakeCapsuleClient.ApplyTyped(ctx, obj)
}

func TestWaitForBlockNodeCapsuleReady_FailureRestoresPreviousSpec(t *testing.T) {
	fake := capsuleStatus(string(operatorv1alpha1.PhaseFailed), "False", 2, 2)
	previous := &operatorv1alpha1.HelmCapsuleSpec{Chart: operatorv1alpha1.HelmChartSpec{Chart: "block-node-server", Version: "0.40.0"}}
	restore := &blockNodeCapsuleRestore{captured: true, spec: previous}
	provider := func(context.Context) (CapsuleKubeClient, error) { return readyOnApply{fake}, nil }

	rpt := runWaitReadyWith(t, provider, restore)

	assert.Equal(t, automa.StatusFailed, rpt.Status, "a restored spec does not make the upgrade succeed")
	require.Len(t, fake.appliedObjs, 1)
	capsule, ok := fake.appliedObjs[0].(*operatorv1alpha1.HelmCapsule)
	require.True(t, ok)
	assert.Equal(t, *previous, capsule.Spec)
}

func TestWaitForBlockNodeCapsuleReady_FailureWithoutPreviousDeletesCapsule(t *testing.T) {
	fake := capsuleStatus(string(operatorv1alpha1.PhaseFailed), "False", 1, 1)
	fake.existing = map[string]string{"block-node": ""}
	restore := &blockNodeCapsuleRestore{captured: true}

	rpt := runWaitReadyWith(t, fake.provider(), restore)

	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, []string{"block-node"}, fake.deleted)
	assert.Empty(t, fake.applied)
}

func TestWaitForBlockNodeCapsuleReady_FailureNotCapturedLeavesCapsule(t *testing.T) {
	fake := capsuleStatus(string(operatorv1alpha1.PhaseFailed), "False", 2, 2)
	fake.existing = map[string]string{"block-node": ""}

	rpt := runWaitReadyWith(t, fake.provider(), &blockNodeCapsuleRestore{})

	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Empty(t, fake.deleted)
	assert.Empty(t, fake.applied)
}

func TestBlockNodeLedgerId(t *testing.T) {
	for profile, want := range map[string]string{
		models.ProfileMainnet:    "0x00",
		models.ProfileTestnet:    "0x01",
		models.ProfilePreviewnet: "0x02",
		models.ProfilePerfnet:    "0x01",
		models.ProfileLocal:      "0x01",
	} {
		assert.Equal(t, want, blockNodeLedgerId(profile), profile)
	}
}

func TestEnsureBlockNodeOrbit_SetsLedgerId(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	ins := bnCapsuleInputs()
	ins.Profile = models.ProfileMainnet

	step, err := EnsureBlockNodeOrbit(ins, fake.provider()).Build()
	require.NoError(t, err)
	rpt := step.Execute(context.Background())

	require.Equal(t, automa.StatusSuccess, rpt.Status, "%v", rpt.Error)
	require.Len(t, fake.appliedObjs, 1)
	orbit, ok := fake.appliedObjs[0].(*operatorv1alpha1.Orbit)
	require.True(t, ok)
	assert.Equal(t, "0x00", orbit.Spec.Consensus.Genesis.AddressBook.LedgerId)
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
