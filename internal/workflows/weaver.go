// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

func CheckWeaverInstallationWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("check-weaver-installation-workflow").Steps(
		steps.CheckWeaverInstallation(core.Paths().BinDir),
	)
}

func NewSelfInstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("self-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.SetupHomeDirectoryStructure(core.Paths()),
		steps.InstallWeaver(core.Paths().BinDir),
	)
}

func NewSelfUninstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("self-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.UninstallWeaver(core.Paths().BinDir),
	)
}
