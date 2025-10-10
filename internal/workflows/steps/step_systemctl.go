package steps

import "github.com/automa-saga/automa"

func DaemonReload() automa.Builder {
	return bashSteps.DaemonReload()
}

// SetupCrio2 sets up CRI-O container runtime
// it is going to be obsolete soon
func SetupCrio2() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-crio").Steps(
		bashSteps.DownloadCrio(),
		bashSteps.InstallCrio(),
		bashSteps.DownloadDasel(),
		bashSteps.InstallDasel(),
		bashSteps.ConfigureSandboxCrio(),
	)
}

// EnableAndStartCrio2 EnableAndStartCrio enables and starts CRI-O service
// it is going to be obsolete soon
func EnableAndStartCrio2() automa.Builder {
	return bashSteps.EnableAndStartCrio()
}
