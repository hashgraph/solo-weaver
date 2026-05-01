// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node.
func NewBlockNodePreflightCheckWorkflow(profile string) *automa.WorkflowBuilder {
	return NewNodeSafetyCheckWorkflow(models.NodeTypeBlock, profile, false)
}
