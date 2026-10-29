// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/models"

	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestBlockNodeInputs creates a BlockNodeInputs with sensible defaults
// matching what the production CLI would provide.
func newTestBlockNodeInputs(profile string) models.BlockNodeInputs {
	return models.BlockNodeInputs{
		Profile:      profile,
		Namespace:    deps.BLOCK_NODE_NAMESPACE,
		Release:      deps.BLOCK_NODE_RELEASE,
		Chart:        deps.BLOCK_NODE_CHART,
		ChartVersion: deps.BLOCK_NODE_VERSION,
		Storage: models.BlockNodeStorage{
			BasePath: deps.BLOCK_NODE_STORAGE_BASE_PATH,
		},
	}
}

func TestSetupBlockNode_FreshInstall(t *testing.T) {
	// Check if system has at least 16GB memory for block node
	hostProfile := hardware.GetHostProfile()
	totalMemoryGB := hostProfile.GetTotalMemoryGB()
	if totalMemoryGB < 16 {
		t.Skipf("Skipping test: Block node requires at least 16GB memory, but system has only %dGB", totalMemoryGB)
	}

	//
	// Given
	//

	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupMetalLBLevel)
	inputs := newTestBlockNodeInputs(models.ProfileMainnet)

	wb := SetupBlockNode(inputs)
	require.NotNil(t, wb)

	workflow, err := wb.Build()
	require.NoError(t, err)
	require.NotNil(t, workflow)

	//
	// When
	//

	report := workflow.Execute(context.Background())

	//
	// Then
	//

	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, SetupBlockNodeStepId, workflow.Id())

	// Verify all substeps were executed successfully
	require.Len(t, report.StepReports, 8, "Expected 8 substeps: ensure-hedera-owner, storage, namespace, PVs, ensure-orbit, create-capsule, wait-ready, verify-reachable")

	// Verify hedera owner step
	hederaReport := report.StepReports[0]
	require.Equal(t, "ensure-hedera-owner", hederaReport.Id)
	require.Equal(t, automa.StatusSuccess, hederaReport.Status)

	// Verify storage setup step
	storageReport := report.StepReports[1]
	require.Equal(t, SetupBlockNodeStorageStepId, storageReport.Id)
	require.Equal(t, automa.StatusSuccess, storageReport.Status)
	require.Equal(t, "true", storageReport.Metadata[ConfiguredByThisStep])

	// Verify namespace creation step
	namespaceReport := report.StepReports[2]
	require.Equal(t, CreateBlockNodeNamespaceStepId, namespaceReport.Id)
	require.Equal(t, automa.StatusSuccess, namespaceReport.Status)
	require.Equal(t, "true", namespaceReport.Metadata[ConfiguredByThisStep])

	// Verify PVs creation step
	pvsReport := report.StepReports[3]
	require.Equal(t, CreateBlockNodePVsStepId, pvsReport.Id)
	require.Equal(t, automa.StatusSuccess, pvsReport.Status)
	require.Equal(t, "true", pvsReport.Metadata[ConfiguredByThisStep])

	// Verify Orbit-ensure step (operator-driven install prerequisite, #1319)
	orbitReport := report.StepReports[4]
	require.Equal(t, EnsureBlockNodeOrbitStepId, orbitReport.Id)
	require.Equal(t, automa.StatusSuccess, orbitReport.Status)

	// Verify HelmCapsule apply step
	capsuleReport := report.StepReports[5]
	require.Equal(t, CreateBlockNodeCapsuleStepId, capsuleReport.Id)
	require.Equal(t, automa.StatusSuccess, capsuleReport.Status)
	require.Equal(t, "true", capsuleReport.Metadata[InstalledByThisStep])

	// Verify wait-for-ready step
	waitReport := report.StepReports[6]
	require.Equal(t, WaitForBlockNodeCapsuleReadyStepId, waitReport.Id)
	require.Equal(t, automa.StatusSuccess, waitReport.Status)

	// Verify reachability probe step (no-ops when LoadBalancerEnabled is false,
	// still reports success — see blocknode.Manager.VerifyExternalReachable).
	verifyReport := report.StepReports[7]
	require.Equal(t, VerifyBlockNodeReachableStepId, verifyReport.Id)
	require.Equal(t, automa.StatusSuccess, verifyReport.Status)
}

func TestSetupBlockNodeLocal_FreshInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy cluster-creation test in short mode")
	}

	//
	// Given
	//

	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupMetalLBLevel)

	inputs := newTestBlockNodeInputs(models.ProfileLocal)

	wb := SetupBlockNode(inputs)
	require.NotNil(t, wb)

	workflow, err := wb.Build()
	require.NoError(t, err)
	require.NotNil(t, workflow)

	//
	// When
	//

	report := workflow.Execute(context.Background())

	//
	// Then
	//

	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, SetupBlockNodeStepId, workflow.Id())

	// Verify all substeps were executed successfully
	require.Len(t, report.StepReports, 8, "Expected 8 substeps: ensure-hedera-owner, storage, namespace, PVs, ensure-orbit, create-capsule, wait-ready, verify-reachable")

	// Verify hedera owner step
	hederaReport := report.StepReports[0]
	require.Equal(t, "ensure-hedera-owner", hederaReport.Id)
	require.Equal(t, automa.StatusSuccess, hederaReport.Status)

	// Verify storage setup step
	storageReport := report.StepReports[1]
	require.Equal(t, SetupBlockNodeStorageStepId, storageReport.Id)
	require.Equal(t, automa.StatusSuccess, storageReport.Status)
	require.Equal(t, "true", storageReport.Metadata[ConfiguredByThisStep])

	// Verify namespace creation step
	namespaceReport := report.StepReports[2]
	require.Equal(t, CreateBlockNodeNamespaceStepId, namespaceReport.Id)
	require.Equal(t, automa.StatusSuccess, namespaceReport.Status)
	require.Equal(t, "true", namespaceReport.Metadata[ConfiguredByThisStep])

	// Verify PVs creation step
	pvsReport := report.StepReports[3]
	require.Equal(t, CreateBlockNodePVsStepId, pvsReport.Id)
	require.Equal(t, automa.StatusSuccess, pvsReport.Status)
	require.Equal(t, "true", pvsReport.Metadata[ConfiguredByThisStep])

	// Verify Orbit-ensure step (operator-driven install prerequisite, #1319)
	orbitReport := report.StepReports[4]
	require.Equal(t, EnsureBlockNodeOrbitStepId, orbitReport.Id)
	require.Equal(t, automa.StatusSuccess, orbitReport.Status)

	// Verify HelmCapsule apply step
	capsuleReport := report.StepReports[5]
	require.Equal(t, CreateBlockNodeCapsuleStepId, capsuleReport.Id)
	require.Equal(t, automa.StatusSuccess, capsuleReport.Status)
	require.Equal(t, "true", capsuleReport.Metadata[InstalledByThisStep])

	// Verify wait-for-ready step
	waitReport := report.StepReports[6]
	require.Equal(t, WaitForBlockNodeCapsuleReadyStepId, waitReport.Id)
	require.Equal(t, automa.StatusSuccess, waitReport.Status)

	// Verify reachability probe step (no-ops when LoadBalancerEnabled is false,
	// still reports success — see blocknode.Manager.VerifyExternalReachable).
	verifyReport := report.StepReports[7]
	require.Equal(t, VerifyBlockNodeReachableStepId, verifyReport.Id)
	require.Equal(t, automa.StatusSuccess, verifyReport.Status)
}

func TestSetupBlockNodeLocal_Idempotency(t *testing.T) {
	//
	// Given - already installed from fresh install test
	//

	inputs := newTestBlockNodeInputs(models.ProfileLocal)

	wb := SetupBlockNode(inputs)
	require.NotNil(t, wb)

	workflow, err := wb.Build()
	require.NoError(t, err)
	require.NotNil(t, workflow)

	//
	// When - run the workflow again on already installed system
	//

	report := workflow.Execute(context.Background())

	//
	// Then - should succeed without errors (idempotent)
	//

	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, SetupBlockNodeStepId, workflow.Id())

	// Verify all substeps still complete successfully
	require.Len(t, report.StepReports, 8, "Expected 8 substeps: ensure-hedera-owner, storage, namespace, PVs, ensure-orbit, create-capsule, wait-ready, verify-reachable")

	// All steps should succeed (idempotent)
	for _, stepReport := range report.StepReports {
		require.Equal(t, automa.StatusSuccess, stepReport.Status, "Step %s should succeed", stepReport.Id)
	}

	// Verify Orbit-ensure step reports the Orbit already exists on re-run. The
	// HelmCapsule apply (CreateBlockNodeCapsule) is itself idempotent via SSA, so
	// a re-run simply re-applies it; the already-exists signal is on the Orbit.
	orbitReport := report.StepReports[4]
	require.Equal(t, EnsureBlockNodeOrbitStepId, orbitReport.Id)
	require.Equal(t, "true", orbitReport.Metadata[AlreadyInstalled], "Orbit should already exist")
}

func TestResetBlockNode_Success(t *testing.T) {
	// Check if system has at least 16GB memory for block node
	hostProfile := hardware.GetHostProfile()
	totalMemoryGB := hostProfile.GetTotalMemoryGB()
	if totalMemoryGB < 16 {
		t.Skipf("Skipping test: Block node requires at least 16GB memory, but system has only %dGB", totalMemoryGB)
	}

	//
	// Given - ensure block node is installed first
	//

	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupMetalLBLevel)

	// Install block node first
	inputs := newTestBlockNodeInputs(models.ProfileLocal)

	installWb := SetupBlockNode(inputs)
	require.NotNil(t, installWb)
	installWorkflow, err := installWb.Build()
	require.NoError(t, err)
	installReport := installWorkflow.Execute(context.Background())
	require.NoError(t, installReport.Error, "Block node must be installed before reset test")
	require.Equal(t, automa.StatusSuccess, installReport.Status)

	// Now test the reset workflow
	wb := ResetBlockNode(inputs.Storage, inputs)
	require.NotNil(t, wb)

	workflow, err := wb.Build()
	require.NoError(t, err)
	require.NotNil(t, workflow)

	//
	// When
	//

	report := workflow.Execute(context.Background())

	//
	// Then
	//

	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, ResetBlockNodeStepId, workflow.Id())

	// Verify all substeps were executed successfully
	require.Len(t, report.StepReports, 5, "Expected 5 substeps: scale-down, wait-terminated, clear-storage, scale-up, wait-ready")

	// Verify scale down step
	scaleDownReport := report.StepReports[0]
	require.Equal(t, ScaleDownBlockNodeStepId, scaleDownReport.Id)
	require.Equal(t, automa.StatusSuccess, scaleDownReport.Status)

	// Verify wait for terminated step
	waitTerminatedReport := report.StepReports[1]
	require.Equal(t, WaitForBlockNodeTerminatedStepId, waitTerminatedReport.Id)
	require.Equal(t, automa.StatusSuccess, waitTerminatedReport.Status)

	// Verify clear storage step
	clearStorageReport := report.StepReports[2]
	require.Equal(t, ClearBlockNodeStorageStepId, clearStorageReport.Id)
	require.Equal(t, automa.StatusSuccess, clearStorageReport.Status)

	// Verify scale up step
	scaleUpReport := report.StepReports[3]
	require.Equal(t, ScaleUpBlockNodeStepId, scaleUpReport.Id)
	require.Equal(t, automa.StatusSuccess, scaleUpReport.Status)

	// Verify wait for ready step
	waitReadyReport := report.StepReports[4]
	require.Equal(t, WaitForBlockNodeStepId, waitReadyReport.Id)
	require.Equal(t, automa.StatusSuccess, waitReadyReport.Status)
}
