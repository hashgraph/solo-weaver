// SPDX-License-Identifier: Apache-2.0

//go:build linux

package blocknode

// adapters_linux.go contains the step-adapter shims that delegate to the real
// workflow/step implementations.  These functions import packages that
// transitively depend on linux-only syscalls (internal/mount), so they are
// compiled only on Linux.
//
// On non-Linux platforms, adapters_stub.go provides no-op stubs so that
// handler unit tests can build and run without a Linux environment.

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

func setupBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.SetupBlockNode(ins)
}

func upgradeBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.UpgradeBlockNode(ins)
}

func resetBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.ResetBlockNode(ins)
}

func uninstallBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.UninstallBlockNode(ins)
}

func purgeBlockNodeStorage(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.PurgeBlockNodeStorage(ins)
}

func installClusterWorkflow(nodeType string, profile string, skipHW bool, sm state.Manager) *automa.WorkflowBuilder {
	return workflows.InstallClusterWorkflow(nodeType, profile, skipHW, sm)
}
