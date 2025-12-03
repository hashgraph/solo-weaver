// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/workflows/steps"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node
func NewBlockNodePreflightCheckWorkflow(profile string) *automa.WorkflowBuilder {
	return NewNodeSafetyCheckWorkflow(core.NodeTypeBlock, profile)
}

// NewBlockNodeInstallWorkflow creates a comprehensive install workflow for block node
func NewBlockNodeInstallWorkflow(profile string, valuesFile string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("block-node-install").Steps(
		NewBlockNodePreflightCheckWorkflow(profile),
		NewNodeSetupWorkflow(core.NodeTypeBlock, profile),
		NewSetupClusterWorkflow(),
		steps.SetupBlockNode(profile, valuesFile),
	)
}
