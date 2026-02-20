// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node.
func NewBlockNodePreflightCheckWorkflow(profile string) *automa.WorkflowBuilder {
	return NewNodeSafetyCheckWorkflow(core.NodeTypeBlock, profile, false)
}

// NewBlockNodeInstallWorkflow creates a comprehensive install workflow for block node
func NewBlockNodeInstallWorkflow(inputs core.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
		InstallClusterWorkflow(core.NodeTypeBlock, inputs.Profile, inputs.SkipHardwareChecks),
		steps.SetupBlockNode(inputs),
	)
}

// NewBlockNodeUpgradeWorkflow creates an upgrade workflow for block node
func NewBlockNodeUpgradeWorkflow(inputs core.BlocknodeInputs, withReset bool) *automa.WorkflowBuilder {
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
func NewBlockNodeResetWorkflow(inputs core.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-reset").Steps(
		steps.ResetBlockNode(inputs),
	)
}
