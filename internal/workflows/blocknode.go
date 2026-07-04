// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node.
func NewBlockNodePreflightCheckWorkflow(spec hardware.DeploymentSpec) *automa.WorkflowBuilder {
	if spec.NodeType == "" {
		spec.NodeType = models.NodeTypeBlock
	}
	return NewNodeSafetyCheckWorkflow(spec, false)
}
