// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// NewTeleportNodeAgentInstallWorkflow creates a workflow to install the Teleport node agent.
// This installs the host-level SSH agent for secure SSH access via Teleport.
func NewTeleportNodeAgentInstallWorkflow() *automa.WorkflowBuilder {
	return steps.SetupTeleportNodeAgent()
}

// NewTeleportClusterAgentInstallWorkflow creates a workflow to install the Teleport Kubernetes cluster agent.
// This provides secure kubectl access via Teleport with full audit logging.
func NewTeleportClusterAgentInstallWorkflow() *automa.WorkflowBuilder {
	return steps.SetupTeleportClusterAgent()
}
