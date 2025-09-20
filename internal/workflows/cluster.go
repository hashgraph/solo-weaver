package workflows

import "github.com/automa-saga/automa"

func SetupClusterWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("setup-kubernetes").Steps(
		SetupWorkflow(),
		//steps.DisableSwap(),
	)
}
