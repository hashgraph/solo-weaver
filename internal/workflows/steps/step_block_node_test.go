//go:build integration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/blocknode"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/pkg/hardware"
)

func TestBlockNodeConstants(t *testing.T) {
	assert.Equal(t, "block-node-block-node-server", blocknode.ServiceName)
	assert.Equal(t, "metallb.io/address-pool=public-address-pool", blocknode.MetalLBAnnotation)
	assert.Equal(t, "block", core.NodeTypeBlock)
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

	reset(t)
	setupPrerequisitesToLevel(t, SetupMetalLBLevel)

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

	reset(t)
	setupPrerequisitesToLevel(t, SetupMetalLBLevel)

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
