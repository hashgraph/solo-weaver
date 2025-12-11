// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node
func NewBlockNodePreflightCheckWorkflow(profile string) *automa.WorkflowBuilder {
	return NewNodeSafetyCheckWorkflow(core.NodeTypeBlock, profile)
}

// NewBlockNodeInstallWorkflow creates a comprehensive install workflow for block node
func NewBlockNodeInstallWorkflow(profile string, valuesFile string, opts ClusterSetupOptions) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
		NewNodeSetupWorkflow(core.NodeTypeBlock, profile),
		NewSetupClusterWorkflow(opts),
		steps.SetupBlockNode(profile, valuesFile),
	)
}

// NewBlockNodeUpgradeWorkflow creates an upgrade workflow for block node
func NewBlockNodeUpgradeWorkflow(profile string, valuesFile string, reuseValues bool) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-upgrade").Steps(
		steps.UpgradeBlockNode(profile, valuesFile, reuseValues),
	)
}
