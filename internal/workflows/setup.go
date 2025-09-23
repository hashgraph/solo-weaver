package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func SetupWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("setup").Steps(
		// NewNodeSafetyCheckWorkflow(),
		steps.SetupHomeDirectoryStructure(),
		steps.RefreshSystemPackageIndex(),
		steps.InstallSystemPackage("iptables", software.NewIptables),
		steps.InstallSystemPackage("gnupg2", software.NewGnupg2),
		//steps.InstallConntrack(),
		//steps.InstallEBTables(),
		//steps.InstallSoCat(),
		//steps.InstallNFTables(),
		//steps.InstallKernelModules(),
		//steps.RemoveExistingContainerd(),
		//steps.RemoveUnusedSystemPackages(),
	)
}
