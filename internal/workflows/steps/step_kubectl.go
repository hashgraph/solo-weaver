package steps

import (
	"github.com/automa-saga/automa"
)

func SetupKubectl() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubectl").Steps(
		bashSteps.DownloadKubectl(),
		bashSteps.InstallKubectl(),
	)
}
