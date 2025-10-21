package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

// NewNodeSetupWorkflow creates a comprehensive setup workflow for any node type
// It runs preflight checks first, then performs the actual setup
func NewNodeSetupWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkflowBuilder().
		WithId(nodeType+"-node-setup").
		Steps(
			// First run preflight checks to ensure system readiness
			NewNodeSafetyCheckWorkflow(nodeType),
			// Then perform the actual setup
			steps.SetupHomeDirectoryStructure(core.Paths()),
			steps.RefreshSystemPackageIndex(),
			steps.InstallSystemPackage("iptables", software.NewIptables),
			steps.InstallSystemPackage("gpg", software.NewGpg),
			steps.InstallSystemPackage("conntrack", software.NewConntrack),
			steps.InstallSystemPackage("ebtables", software.NewEbtables),
			steps.InstallSystemPackage("socat", software.NewSocat),
			steps.InstallSystemPackage("nftables", software.NewNftables),
			steps.SetupSystemdService("nftables"),
			steps.InstallKernelModule("overlay"),
			steps.InstallKernelModule("br_netfilter"),
			steps.AutoRemoveOrphanedPackages(),
			//steps.RemoveSystemPackage("containerd", software.NewContainerd),
		)
}
