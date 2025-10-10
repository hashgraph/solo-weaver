package steps

import (
	"github.com/automa-saga/automa"
)

func SetupMetalLB() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-metallb").Steps(
		bashSteps.InstallMetalLB(),
		bashSteps.ConfigureMetalLB(),
	)
}
