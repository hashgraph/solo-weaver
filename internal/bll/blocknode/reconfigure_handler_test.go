// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// assertResolutionMentions extracts the ErrPropertyResolution from the given
// errorx error and asserts it contains the given substring. The resolution is
// metadata-only — err.Error() does NOT include it — so we have to pull it out.
func assertResolutionMentions(t *testing.T, err error, want string) {
	t.Helper()
	raw, ok := errorx.ExtractProperty(err, models.ErrPropertyResolution)
	require.True(t, ok, "expected error to carry a resolution property")
	resolution, ok := raw.(string)
	require.True(t, ok, "expected resolution property to be a string, got %T", raw)
	assert.Contains(t, resolution, want)
}

// newMinimalReconfigureHandler returns a handler with only the fields used
// by BuildWorkflow (runtime is not accessed during workflow construction).
func newMinimalReconfigureHandler() *ReconfigureHandler {
	return &ReconfigureHandler{
		BaseHandler: bll.BaseHandler[models.BlockNodeInputs]{},
		runtime:     nil,
	}
}

// deployedBlockNodeState returns a state where the block node is deployed at
// the given storage base path.
func deployedBlockNodeState(basePath string) state.State {
	return state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{
					Status: release.StatusDeployed,
				},
				Storage: models.BlockNodeStorage{
					BasePath: basePath,
				},
			},
		},
	}
}

// reconfigureInputs returns minimal valid UserInputs for a reconfigure.
func reconfigureInputs(basePath string, resetStorage bool) models.UserInputs[models.BlockNodeInputs] {
	return reconfigureInputsWithFlags(basePath, resetStorage, false)
}

// reconfigureInputsWithFlags is the full-flag variant for tests that need to
// exercise --purge-storage paths.
func reconfigureInputsWithFlags(basePath string, resetStorage, purgeStorage bool) models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node-ns",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.30.0",
			Storage: models.BlockNodeStorage{
				BasePath: basePath,
			},
			ResetStorage: resetStorage || purgeStorage,
			PurgeStorage: purgeStorage,
			ReuseValues:  true,
		},
	}
}

// workflowStepIDs builds the given WorkflowBuilder and returns the IDs of the
// top-level child steps in execution order.
func workflowStepIDs(t *testing.T, wb *automa.WorkflowBuilder) []string {
	t.Helper()
	stp, err := wb.Build()
	require.NoError(t, err)

	wf, ok := stp.(automa.Workflow)
	require.True(t, ok, "expected a Workflow, got %T", stp)

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	return ids
}

// TestBuildWorkflow_WithReset_PathsUnchanged_DataOnly verifies that
// --with-reset wipes data but leaves PVs/PVCs intact: purge → upgrade.
func TestBuildWorkflow_WithReset_PathsUnchanged_DataOnly(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/storage")
	inputs := reconfigureInputs("/mnt/storage", true)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-reconfigure-with-reset", wb.Id())

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, []string{
		steps.PurgeBlockNodeStorageStepId,
		steps.UpgradeBlockNodeStepId,
	}, ids)
}

// TestBuildWorkflow_WithReset_PathsChanged_ReturnsError verifies that
// --with-reset alone is rejected when storage paths change; the operator
// must now use --purge-storage to delete and recreate PVs/PVCs.
func TestBuildWorkflow_WithReset_PathsChanged_ReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/old")
	inputs := reconfigureInputs("/mnt/new", true)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestBuildWorkflow_PurgeStorage_IncludesRecreateStep verifies that
// --purge-storage triggers the full purge → recreate → upgrade chain
// (which deletes PVs/PVCs, creates new ones at the new paths, then upgrades).
func TestBuildWorkflow_PurgeStorage_IncludesRecreateStep(t *testing.T) {
	h := newMinimalReconfigureHandler()

	for _, tc := range []struct {
		name    string
		oldBase string
		newBase string
	}{
		{name: "paths_changed", oldBase: "/mnt/old", newBase: "/mnt/new"},
		{name: "paths_unchanged", oldBase: "/mnt/storage", newBase: "/mnt/storage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			currentState := deployedBlockNodeState(tc.oldBase)
			inputs := reconfigureInputsWithFlags(tc.newBase, false, true)

			wb, err := h.BuildWorkflow(currentState, inputs)

			require.NoError(t, err)
			require.NotNil(t, wb)
			assert.Equal(t, "block-node-reconfigure-purge-storage", wb.Id())

			ids := workflowStepIDs(t, wb)
			assert.Equal(t, []string{
				steps.PurgeBlockNodeStorageStepId,
				steps.RecreateBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
			}, ids)
		})
	}
}

// TestBuildWorkflow_NoReset_SamePathsUpgradeAndRestart verifies the default
// non-reset path: UpgradeBlockNode + RolloutRestartBlockNode.
func TestBuildWorkflow_NoReset_SamePathsUpgradeAndRestart(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/storage")
	inputs := reconfigureInputs("/mnt/storage", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.NoError(t, err)
	assert.Equal(t, "block-node-reconfigure", wb.Id())

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, []string{
		steps.UpgradeBlockNodeStepId,
		steps.RolloutRestartBlockNodeStepId,
	}, ids)
}

// TestBuildWorkflow_NoReset_ChangedPathsReturnsError verifies that changing
// storage paths without --purge-storage is blocked with a clear error that
// points at the right flag.
func TestBuildWorkflow_NoReset_ChangedPathsReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/old")
	inputs := reconfigureInputs("/mnt/new", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestBuildWorkflow_NotInstalled_ReturnsError verifies the guard condition.
func TestBuildWorkflow_NotInstalled_ReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := state.State{} // zero value → status is not StatusDeployed
	inputs := reconfigureInputs("/mnt/storage", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "block node is not installed")
}
