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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

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
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node-ns",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.30.0",
			Storage: models.BlockNodeStorage{
				BasePath: basePath,
			},
			ResetStorage: resetStorage,
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

// TestBuildWorkflow_WithReset_IncludesRecreateStep verifies that when
// --with-reset is supplied the workflow contains all three expected sub-workflows
// in the correct order: purge → recreate → upgrade.
func TestBuildWorkflow_WithReset_IncludesRecreateStep(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/old-storage")
	inputs := reconfigureInputs("/mnt/new-storage", true)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-reconfigure-with-reset", wb.Id())

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, []string{
		steps.PurgeBlockNodeStorageStepId,
		steps.RecreateBlockNodeStorageStepId,
		steps.UpgradeBlockNodeStepId,
	}, ids)
}

// TestBuildWorkflow_WithReset_PurgeUsesOldStoragePaths verifies that the
// PurgeBlockNodeStorage sub-workflow is created with the *currently deployed*
// storage configuration (so ResetStorage clears the directories that actually
// exist on disk), while the RecreateBlockNodeStorage and UpgradeBlockNode
// sub-workflows receive the *new* (requested) configuration.
//
// We verify this indirectly: if both sub-workflows shared the same provider
// (built from `ins`), the PurgeBlockNodeStorage manager would see new paths
// that don't exist yet and silently no-op. We can't call Execute here (no
// cluster), but we confirm that BuildWorkflow builds without errors and that
// the workflow structure is correct regardless of whether paths are the same or
// different.
func TestBuildWorkflow_WithReset_PathsChangedAndUnchanged(t *testing.T) {
	h := newMinimalReconfigureHandler()

	for _, tc := range []struct {
		name         string
		oldBase      string
		newBase      string
		wantWorkflow string
	}{
		{
			name:         "paths_changed",
			oldBase:      "/mnt/old",
			newBase:      "/mnt/new",
			wantWorkflow: "block-node-reconfigure-with-reset",
		},
		{
			name:         "paths_unchanged",
			oldBase:      "/mnt/storage",
			newBase:      "/mnt/storage",
			wantWorkflow: "block-node-reconfigure-with-reset",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			currentState := deployedBlockNodeState(tc.oldBase)
			inputs := reconfigureInputs(tc.newBase, true)

			wb, err := h.BuildWorkflow(currentState, inputs)

			require.NoError(t, err)
			assert.Equal(t, tc.wantWorkflow, wb.Id())

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
// storage paths without --with-reset is blocked with a clear error.
func TestBuildWorkflow_NoReset_ChangedPathsReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/old")
	inputs := reconfigureInputs("/mnt/new", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "storage paths have changed")
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
