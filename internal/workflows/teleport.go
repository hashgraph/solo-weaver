// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// NewTeleportNodeAgentInstallWorkflow creates a workflow to install the Teleport node agent.
// sm is the shared Manager used to record installation progress.
func NewTeleportNodeAgentInstallWorkflow(mr software.MachineRuntime) *automa.WorkflowBuilder {
	return steps.SetupTeleportNodeAgent(mr)
}

// NewTeleportClusterAgentInstallWorkflow creates a workflow to install the Teleport Kubernetes cluster agent.
// This provides secure kubectl access via Teleport with full audit logging.
func NewTeleportClusterAgentInstallWorkflow() *automa.WorkflowBuilder {
	return steps.SetupTeleportClusterAgent()
}

// NewTeleportClusterAgentUninstallWorkflow creates a workflow to uninstall the Teleport Kubernetes cluster agent.
func NewTeleportClusterAgentUninstallWorkflow() *automa.WorkflowBuilder {
	return steps.TeardownTeleportClusterAgent()
}

// NewTeleportNodeAgentUninstallWorkflow creates a workflow to uninstall the Teleport node agent.
// mr provides software state needed by the installer.
func NewTeleportNodeAgentUninstallWorkflow(mr software.MachineRuntime) *automa.WorkflowBuilder {
	return steps.TeardownTeleportNodeAgent(mr)
}
