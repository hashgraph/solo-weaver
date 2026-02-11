// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	wb := SetupBlockNode(core.ProfileMainnet, "")
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
	require.Len(t, report.StepReports, 6, "Expected 6 substeps: storage, namespace, PVs, install, annotate, wait")

	// Verify storage setup step
	storageReport := report.StepReports[0]
	require.Equal(t, SetupBlockNodeStorageStepId, storageReport.Id)
	require.Equal(t, automa.StatusSuccess, storageReport.Status)
	require.Equal(t, "true", storageReport.Metadata[ConfiguredByThisStep])

	// Verify namespace creation step
	namespaceReport := report.StepReports[1]
	require.Equal(t, CreateBlockNodeNamespaceStepId, namespaceReport.Id)
	require.Equal(t, automa.StatusSuccess, namespaceReport.Status)
	require.Equal(t, "true", namespaceReport.Metadata[ConfiguredByThisStep])

	// Verify PVs creation step
	pvsReport := report.StepReports[2]
	require.Equal(t, CreateBlockNodePVsStepId, pvsReport.Id)
	require.Equal(t, automa.StatusSuccess, pvsReport.Status)
	require.Equal(t, "true", pvsReport.Metadata[ConfiguredByThisStep])

	// Verify helm chart installation step
	installReport := report.StepReports[3]
	require.Equal(t, InstallBlockNodeStepId, installReport.Id)
	require.Equal(t, automa.StatusSuccess, installReport.Status)
	require.Equal(t, "true", installReport.Metadata[InstalledByThisStep])

	// Verify service annotation step
	annotateReport := report.StepReports[4]
	require.Equal(t, AnnotateBlockNodeServiceStepId, annotateReport.Id)
	require.Equal(t, automa.StatusSuccess, annotateReport.Status)
	require.Equal(t, "true", annotateReport.Metadata[ConfiguredByThisStep])

	// Verify wait step
	waitReport := report.StepReports[5]
	require.Equal(t, WaitForBlockNodeStepId, waitReport.Id)
	require.Equal(t, automa.StatusSuccess, waitReport.Status)
}

func TestSetupBlockNodeLocal_FreshInstall(t *testing.T) {
	//
	// Given
	//

	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupMetalLBLevel)

	wb := SetupBlockNode(core.ProfileLocal, "")
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
	require.Len(t, report.StepReports, 6, "Expected 6 substeps: storage, namespace, PVs, install, annotate, wait")

	// Verify storage setup step
	storageReport := report.StepReports[0]
	require.Equal(t, SetupBlockNodeStorageStepId, storageReport.Id)
	require.Equal(t, automa.StatusSuccess, storageReport.Status)
	require.Equal(t, "true", storageReport.Metadata[ConfiguredByThisStep])

	// Verify namespace creation step
	namespaceReport := report.StepReports[1]
	require.Equal(t, CreateBlockNodeNamespaceStepId, namespaceReport.Id)
	require.Equal(t, automa.StatusSuccess, namespaceReport.Status)
	require.Equal(t, "true", namespaceReport.Metadata[ConfiguredByThisStep])

	// Verify PVs creation step
	pvsReport := report.StepReports[2]
	require.Equal(t, CreateBlockNodePVsStepId, pvsReport.Id)
	require.Equal(t, automa.StatusSuccess, pvsReport.Status)
	require.Equal(t, "true", pvsReport.Metadata[ConfiguredByThisStep])

	// Verify helm chart installation step
	installReport := report.StepReports[3]
	require.Equal(t, InstallBlockNodeStepId, installReport.Id)
	require.Equal(t, automa.StatusSuccess, installReport.Status)
	require.Equal(t, "true", installReport.Metadata[InstalledByThisStep])

	// Verify service annotation step
	annotateReport := report.StepReports[4]
	require.Equal(t, AnnotateBlockNodeServiceStepId, annotateReport.Id)
	require.Equal(t, automa.StatusSuccess, annotateReport.Status)
	require.Equal(t, "true", annotateReport.Metadata[ConfiguredByThisStep])

	// Verify wait step
	waitReport := report.StepReports[5]
	require.Equal(t, WaitForBlockNodeStepId, waitReport.Id)
	require.Equal(t, automa.StatusSuccess, waitReport.Status)
}

func TestSetupBlockNodeLocal_Idempotency(t *testing.T) {
	//
	// Given - already installed from fresh install test
	//

	wb := SetupBlockNode(core.ProfileLocal, "")
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
	require.Len(t, report.StepReports, 6, "Expected 6 substeps: storage, namespace, PVs, install, annotate, wait")

	// All steps should succeed (idempotent)
	for _, stepReport := range report.StepReports {
		require.Equal(t, automa.StatusSuccess, stepReport.Status, "Step %s should succeed", stepReport.Id)
	}

	// Verify helm chart installation step reports already installed
	installReport := report.StepReports[3]
	require.Equal(t, InstallBlockNodeStepId, installReport.Id)
	require.Equal(t, "true", installReport.Metadata[AlreadyInstalled], "Block node should already be installed")
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
	installWb := SetupBlockNode(core.ProfileLocal, "")
	require.NotNil(t, installWb)
	installWorkflow, err := installWb.Build()
	require.NoError(t, err)
	installReport := installWorkflow.Execute(context.Background())
	require.NoError(t, installReport.Error, "Block node must be installed before reset test")
	require.Equal(t, automa.StatusSuccess, installReport.Status)

	// Now test the reset workflow
	wb := ResetBlockNode()
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
