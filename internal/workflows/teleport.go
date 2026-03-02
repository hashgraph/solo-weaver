// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// NewTeleportNodeAgentInstallWorkflow creates a workflow to install the Teleport node agent.
// sm is the shared Manager used to record installation progress.
func NewTeleportNodeAgentInstallWorkflow(sm state.Manager) *automa.WorkflowBuilder {
	return steps.SetupTeleportNodeAgent(sm)
}

// NewTeleportClusterAgentInstallWorkflow creates a workflow to install the Teleport Kubernetes cluster agent.
// This provides secure kubectl access via Teleport with full audit logging.
func NewTeleportClusterAgentInstallWorkflow() *automa.WorkflowBuilder {
	return steps.SetupTeleportClusterAgent()
}
