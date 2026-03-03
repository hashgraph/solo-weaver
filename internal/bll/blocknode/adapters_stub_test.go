// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package blocknode

// adapters_stub.go provides no-op step-adapter stubs on non-Linux platforms
// (e.g. macOS during development).  The real implementations live in
// adapters_linux.go and delegate to internal/workflows/steps which
// transitively imports internal/mount (linux-only syscalls).
//
// These stubs return an empty WorkflowBuilder so that BuildWorkflow unit tests
// can assert on precondition guards without executing any real workflow steps.

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

func setupBlockNode(_ models.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("stub-setup-block-node")
}

func upgradeBlockNode(_ models.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("stub-upgrade-block-node")
}

func resetBlockNode(_ models.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("stub-reset-block-node")
}

func uninstallBlockNode(_ models.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("stub-uninstall-block-node")
}

func purgeBlockNodeStorage(_ models.BlocknodeInputs) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("stub-purge-block-node-storage")
}

func installClusterWorkflow(_ string, _ string, _ bool, _ state.Manager) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("stub-install-cluster")
}
