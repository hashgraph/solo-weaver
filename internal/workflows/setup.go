package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
)

func SetupWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("setup").Steps(
		NewSystemSafetyCheckWorkflow(),
		steps.SetupHomeDirectoryStructure(),
		//steps.DisableSwap(),
		steps.InstallIpTables(),
		//steps.InstallGPG(),
		//steps.InstallConntrack(),
		//steps.InstallEBTables(),
		//steps.InstallSoCat(),
		//steps.InstallNFTables(),
		//steps.InstallKernelModules(),
		//steps.RemoveExistingContainerd(),
		//steps.RemoveUnusedPackages(),
	)
}
