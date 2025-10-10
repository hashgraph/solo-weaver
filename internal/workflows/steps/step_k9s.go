package steps

import (
	"github.com/automa-saga/automa"
)

func SetupK9s() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-k9s").Steps(
		bashSteps.DownloadK9s(),
		bashSteps.InstallK9s(),
	)
}
