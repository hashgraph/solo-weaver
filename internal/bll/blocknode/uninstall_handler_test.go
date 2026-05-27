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

func newMinimalUninstallHandler() *UninstallHandler {
	return &UninstallHandler{
		BaseHandler: bll.BaseHandler[models.BlockNodeInputs]{},
		runtime:     nil,
	}
}

func uninstallInputs(resetStorage, purgeStorage bool) models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node-ns",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.30.0",
			ResetStorage: resetStorage || purgeStorage,
			PurgeStorage: purgeStorage,
		},
	}
}

// TestUninstall_NoFlags_HelmOnly verifies the plain uninstall workflow only
// runs UninstallBlockNode (helm release removed, data and PVs/PVCs preserved).
func TestUninstall_NoFlags_HelmOnly(t *testing.T) {
	h := newMinimalUninstallHandler()
	currentState := state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{Status: release.StatusDeployed},
			},
		},
	}

	wb, err := h.BuildWorkflow(currentState, uninstallInputs(false, false))

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-uninstall", wb.Id())
	assert.Equal(t, []string{steps.UninstallBlockNodeStepId}, workflowStepIDs(t, wb))
}

// TestUninstall_WithReset_PurgeThenUninstall verifies --with-reset wipes data
// before uninstalling, but does NOT delete PVs/PVCs.
func TestUninstall_WithReset_PurgeThenUninstall(t *testing.T) {
	h := newMinimalUninstallHandler()
	currentState := state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{Status: release.StatusDeployed},
			},
		},
	}

	wb, err := h.BuildWorkflow(currentState, uninstallInputs(true, false))

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-uninstall-with-reset", wb.Id())
	assert.Equal(t, []string{
		steps.PurgeBlockNodeStorageStepId,
		steps.UninstallBlockNodeStepId,
	}, workflowStepIDs(t, wb))
}

// TestUninstall_PurgeStorage_FullCleanup verifies --purge-storage wipes data,
// deletes PVCs/PVs, and then uninstalls the helm release.
func TestUninstall_PurgeStorage_FullCleanup(t *testing.T) {
	h := newMinimalUninstallHandler()
	currentState := state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{Status: release.StatusDeployed},
			},
		},
	}

	wb, err := h.BuildWorkflow(currentState, uninstallInputs(false, true))

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-uninstall-purge-storage", wb.Id())
	assert.Equal(t, []string{
		steps.PurgeBlockNodeStorageStepId,
		steps.DeleteBlockNodePVsStepId,
		steps.UninstallBlockNodeStepId,
	}, workflowStepIDs(t, wb))
}

// TestUninstall_PurgeImpliesReset confirms passing both --with-reset and
// --purge-storage selects the purge-storage workflow (purge is the superset).
func TestUninstall_PurgeImpliesReset(t *testing.T) {
	h := newMinimalUninstallHandler()
	currentState := state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{Status: release.StatusDeployed},
			},
		},
	}

	wb, err := h.BuildWorkflow(currentState, uninstallInputs(true, true))

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-uninstall-purge-storage", wb.Id())
}
