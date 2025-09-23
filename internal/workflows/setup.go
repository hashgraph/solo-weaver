package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

// NewNodeSetupWorkflow creates a comprehensive setup workflow for any node type
// It runs preflight checks first, then performs the actual setup
func NewNodeSetupWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkFlowBuilder(nodeType+"-node-setup").Steps(
		// First run preflight checks to ensure system readiness
		NewNodeSafetyCheckWorkflow(nodeType),
		// Then perform the actual setup
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
