// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node.
func NewBlockNodePreflightCheckWorkflow(profile string) *automa.WorkflowBuilder {
	return NewNodeSafetyCheckWorkflow(models.NodeTypeBlock, profile, false)
}

// NewBlockNodeInstallWorkflow creates a comprehensive install workflow for block node
func NewBlockNodeInstallWorkflow(inputs models.BlocknodeInputs, sm state.Manager) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
		InstallClusterWorkflow(models.NodeTypeBlock, inputs.Profile, inputs.SkipHardwareChecks, sm),
		steps.SetupBlockNode(inputs),
	)
}

// NewBlockNodeUpgradeWorkflow creates an upgrade workflow for block node
func NewBlockNodeUpgradeWorkflow(inputs models.BlocknodeInputs, withReset bool) *automa.WorkflowBuilder {
	if withReset {
		return automa.NewWorkflowBuilder().WithId("block-node-upgrade-with-reset").Steps(
			steps.PurgeBlockNodeStorage(inputs),
			steps.UpgradeBlockNode(inputs),
		)
	}
	return automa.NewWorkflowBuilder().WithId("block-node-upgrade").Steps(
		steps.UpgradeBlockNode(inputs),
	)
}

// NewBlockNodeResetWorkflow creates a reset workflow for block node
func NewBlockNodeResetWorkflow(inputs models.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-reset").Steps(
		steps.ResetBlockNode(inputs),
	)
}
