// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewSoakStartWorkflow enqueues a consensus-node migration soak start request
// on the running solo-provisioner-daemon.
func NewSoakStartWorkflow(req consensus.SoakStartRequest) *automa.WorkflowBuilder {
	paths := models.Paths()
	return automa.NewWorkflowBuilder().WithId("consensus-soak-start-workflow").Steps(
		steps.SoakStartStep(paths.DaemonSockPath, req),
	)
}

// NewSoakStopWorkflow stops the running consensus-node migration soak watcher.
// When keepState is true the persisted cutover-state.jsonl is left in place so
// the daemon resumes the soak on the next restart.
func NewSoakStopWorkflow(keepState bool) *automa.WorkflowBuilder {
	paths := models.Paths()
	return automa.NewWorkflowBuilder().WithId("consensus-soak-stop-workflow").Steps(
		steps.SoakStopStep(paths.DaemonSockPath, keepState),
	)
}
