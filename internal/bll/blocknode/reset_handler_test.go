// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func newMinimalResetHandler() *ResetHandler {
	return &ResetHandler{
		BaseHandler: bll.BaseHandler[models.BlockNodeInputs]{},
		runtime:     nil,
	}
}

// resetInputs returns minimal valid UserInputs for a reset at the given
// requested storage base path.
func resetInputs(basePath string, purgeStorage bool) models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node-ns",
			Release:      "block-node",
			ChartVersion: "0.30.0",
			Storage:      models.BlockNodeStorage{BasePath: basePath},
			ResetStorage: true,
			PurgeStorage: purgeStorage,
		},
	}
}

// TestReset_WorkflowIds pins the workflow id per branch — the id is what the TUI
// labels the run with and what the acceptance tests assert on.
func TestReset_WorkflowIds(t *testing.T) {
	h := newMinimalResetHandler()

	for _, tc := range []struct {
		name       string
		inputs     models.UserInputs[models.BlockNodeInputs]
		workflowId string
	}{
		{
			name:       "plain_reset",
			inputs:     resetInputs("/mnt/storage", false),
			workflowId: "block-node-reset",
		},
		{
			name:       "purge_storage",
			inputs:     resetInputs("/mnt/new", true),
			workflowId: "block-node-reset-purge-storage",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wb, err := h.BuildWorkflow(deployedBlockNodeState("/mnt/storage"), tc.inputs)

			require.NoError(t, err)
			require.NotNil(t, wb)
			assert.Equal(t, tc.workflowId, wb.Id())
			assert.Equal(t, []string{steps.ResetBlockNodeStepId}, workflowStepIDs(t, wb))
		})
	}
}

// TestReset_PathChangeWithoutPurgeIsRejected covers the guard reset gained along
// with the flag: the storage flags are persistent on `block node`, so a path
// change reaches reset whether or not it can act on one.
func TestReset_PathChangeWithoutPurgeIsRejected(t *testing.T) {
	h := newMinimalResetHandler()

	_, err := h.BuildWorkflow(deployedBlockNodeState("/mnt/storage"), resetInputs("/mnt/new", false))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestReset_NotInstalled_ReturnsError keeps the precondition ahead of the
// storage plan: an operator resetting a host with no release needs the
// "not installed" message, not a storage complaint.
func TestReset_NotInstalled_ReturnsError(t *testing.T) {
	h := newMinimalResetHandler()

	notDeployed := state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{Status: release.StatusUninstalled},
			},
		},
	}

	_, err := h.BuildWorkflow(notDeployed, resetInputs("/mnt/storage", false))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "block node is not installed")
}
