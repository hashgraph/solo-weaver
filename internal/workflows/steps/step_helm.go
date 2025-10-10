package steps

import (
	"github.com/automa-saga/automa"
)

func SetupHelm() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-helm").Steps(
		bashSteps.DownloadHelm(),
		bashSteps.InstallHelm(),
	)
}
