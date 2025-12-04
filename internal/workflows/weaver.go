package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/workflows/steps"
)

func NewSelfInstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("self-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.SetupHomeDirectoryStructure(core.Paths()),
		steps.InstallWeaver(core.Paths().BinDir),
	)
}
