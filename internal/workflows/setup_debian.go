package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps/debian"
)

func SetupDebianOS() automa.Builder {
	return automa.NewWorkFlowBuilder("setup_debian_os").Steps(
		steps.SetupDirectories(),
		debian.UpdateDebianOS(),
		debian.UpgradeDebianOS(),
		debian.DisableSwap(),
		debian.InstallIpTables(),
		debian.InstallGPG(),
		debian.InstallCurl(),
		debian.InstallConntrack(),
		debian.InstallEBTables(),
		debian.InstallSoCat(),
		debian.InstallNFTables(),
		debian.InstallKernelModules(),
		debian.RemoveExistingContainerd(),
		debian.RemoveUnusedPackages(),
	)
}
