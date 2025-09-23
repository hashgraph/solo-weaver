package workflows

import "github.com/automa-saga/automa"

func NewSetupClusterWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkFlowBuilder("setup-kubernetes").Steps(
		NewNodeSetupWorkflow(nodeType),
		//steps.DisableSwap(),
	)
}
