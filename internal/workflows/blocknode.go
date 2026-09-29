// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewBlockNodePreflightCheckWorkflow creates a safety check workflow for block node.
// storage and chartVersion are the effective values `install` would resolve;
// pass the zero storage when they cannot be and the media check skips itself.
func NewBlockNodePreflightCheckWorkflow(spec hardware.DeploymentSpec, storage models.BlockNodeStorage, chartVersion string) *automa.WorkflowBuilder {
	if spec.NodeType == "" {
		spec.NodeType = models.NodeTypeBlock
	}
	// Added here, not inside NewNodeSafetyCheckWorkflow: that's shared with
	// cluster/consensus/alloy checks, none of which have storage paths to classify.
	// It runs first: it's warn-only, and a failed hardware check would skip it.
	// check takes no --values, so it never sees a plugins-baked image.
	return nodeSafetyCheckWorkflow(spec, false,
		steps.BlockNodeStorageMediaStep(storage, chartVersion, true))
}
