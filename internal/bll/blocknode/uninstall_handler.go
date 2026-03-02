// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// UninstallHandler handles the ActionUninstall intent for a block node.
// It optionally purges persistent storage before removing the Helm release.
type UninstallHandler struct {
	base rslAccessor
}

func newUninstallHandler(base rslAccessor) *UninstallHandler {
	return &UninstallHandler{base: base}
}

// PrepareEffectiveInputs for uninstall passes inputs through — no field
// resolution is required since the goal is to remove the deployment.
func (h *UninstallHandler) PrepareEffectiveInputs(
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*models.UserInputs[models.BlocknodeInputs], error) {
	if inputs == nil {
		return nil, errorx.IllegalArgument.New("user inputs cannot be nil")
	}
	return inputs, nil
}

// BuildWorkflow validates that the block node is deployed (unless --force) and
// returns the uninstall workflow.
func (h *UninstallHandler) BuildWorkflow(
	nodeState state.BlockNodeState,
	_ state.ClusterState,
	inputs *models.UserInputs[models.BlocknodeInputs],
) (*automa.WorkflowBuilder, error) {
	if nodeState.ReleaseInfo.Status != release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot uninstall").
			WithProperty(errPropertyResolution,
				"use 'weaver block node install' to install the block node, or pass --force to continue")
	}

	ins := inputs.Custom
	var wb *automa.WorkflowBuilder
	if ins.ResetStorage {
		wb = automa.NewWorkflowBuilder().WithId("block-node-uninstall-with-reset").
			Steps(purgeBlockNodeStorage(ins), uninstallBlockNode(ins))
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-uninstall").
			Steps(uninstallBlockNode(ins))
	}
	return wb, nil
}
