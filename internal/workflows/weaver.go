// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

func CheckWeaverInstallationWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("check-weaver-installation-workflow").Steps(
		steps.CheckWeaverInstallation(core.Paths().BinDir),
	)
}

func NewSelfInstallWorkflow() *automa.WorkflowBuilder {
	wf := automa.NewWorkflowBuilder().WithId("self-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.SetupHomeDirectoryStructure(core.Paths()),
		steps.InstallWeaver(core.Paths().BinDir),
	)

	// Add migration workflow if there are applicable migrations
	migrationWf, err := BuildMigrationWorkflow()
	if err != nil {
		logx.As().Error().
			Err(err).
			Msg("Failed to build migration workflow, skipping migrations")
	} else if migrationWf != nil {
		wf = wf.Steps(migrationWf)
	}

	return wf
}

func NewSelfUninstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("self-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.UninstallWeaver(core.Paths().BinDir),
	)
}
