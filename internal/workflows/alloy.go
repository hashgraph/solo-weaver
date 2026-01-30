// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// NewAlloyInstallWorkflow creates a workflow to install the Alloy observability stack.
// This installs Prometheus Operator CRDs, Node Exporter, and Grafana Alloy.
func NewAlloyInstallWorkflow() *automa.WorkflowBuilder {
	return steps.SetupAlloyStack()
}

// NewAlloyUninstallWorkflow creates a workflow to uninstall the Alloy observability stack.
// This removes Grafana Alloy, Node Exporter, and Prometheus Operator CRDs.
func NewAlloyUninstallWorkflow() *automa.WorkflowBuilder {
	return steps.TeardownAlloyStack()
}
