package steps

import (
	"github.com/automa-saga/automa"
)

func SetupCiliumCNI() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-cilium").Steps(
		bashSteps.DownloadCiliumCli(),
		bashSteps.InstallCiliumCli(),
		bashSteps.ConfigureCiliumCNI(),
		bashSteps.InstallCiliumCNI(),
	)
}

func EnableAndStartCiliumCNI() automa.Builder {
	// break it up into a workflow
	return bashSteps.EnableAndStartCiliumCNI()
}
