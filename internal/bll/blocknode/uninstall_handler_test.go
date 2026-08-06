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

// TestUninstall_NoFlags_HelmOnly verifies the plain uninstall workflow turns the
// daemon's traffic-shaper monitor off, removes the helm release, and then tears
// down the network plane (data and PVs/PVCs are preserved).
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
	assert.Equal(t, []string{
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.UninstallBlockNodeStepId,
		steps.NetworkPolicyDeleteAllStepId,
		steps.NftServiceTeardownStepId,
		steps.TcEgressTeardownStepId,
		steps.TcEgressServiceTeardownStepId,
		steps.TcIngressTeardownStepId,
	}, workflowStepIDs(t, wb))
}

// TestUninstall_WithReset_PurgeThenUninstall verifies --with-reset turns the
// monitor off, wipes data, uninstalls the helm release, and then tears down the
// network plane — PVs/PVCs are NOT deleted.
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
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.PurgeBlockNodeStorageStepId,
		steps.UninstallBlockNodeStepId,
		steps.NetworkPolicyDeleteAllStepId,
		steps.NftServiceTeardownStepId,
		steps.TcEgressTeardownStepId,
		steps.TcEgressServiceTeardownStepId,
		steps.TcIngressTeardownStepId,
	}, workflowStepIDs(t, wb))
}

// TestUninstall_PurgeStorage_FullCleanup verifies --purge-storage turns the
// monitor off, wipes data, deletes PVCs/PVs, uninstalls the helm release, and
// then tears down the network plane.
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
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.PurgeBlockNodeStorageStepId,
		steps.DeleteBlockNodePVsStepId,
		steps.UninstallBlockNodeStepId,
		steps.NetworkPolicyDeleteAllStepId,
		steps.NftServiceTeardownStepId,
		steps.TcEgressTeardownStepId,
		steps.TcEgressServiceTeardownStepId,
		steps.TcIngressTeardownStepId,
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
