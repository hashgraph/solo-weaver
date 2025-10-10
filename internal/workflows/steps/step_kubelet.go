package steps

import (
	"github.com/automa-saga/automa"
)

func SetupKubelet() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubelet").
		Steps(
			bashSteps.DownloadKubelet(),
			bashSteps.InstallKubelet(),
			bashSteps.DownloadKubeletConfig(),
			bashSteps.ConfigureKubelet(),
		)
}

func EnableAndStartKubelet() automa.Builder {
	return bashSteps.EnableAndStartKubelet()
}
