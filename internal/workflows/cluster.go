package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
)

func NewSetupClusterWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkFlowBuilder("setup-kubernetes").Steps(
		NewNodeSetupWorkflow(nodeType),
		steps.DisableSwap(),
		//steps.ConfigureSysctlForKubernetes(),
		//steps.InstallCrio(),
		//steps.InstallCilium(),
		//steps.InstallKubelet(),
		//steps.InstallKubeadm(),
		//steps.InstallKubectl(),
		//steps.InstallHelm(),
		//steps.InstallK9s(),
		//steps.ConfigureKubelet(),
		//steps.EnableAndStartKubelet(),
		//steps.ConfigureKubeadm(),
		//steps.EnableAndStartKubeadm(),
		//steps.ConfigureKubeconfigForAdminUser(),
		//steps.ConfigureCrio(),
		//steps.EnableAndStartCrio(),
		//steps.ConfigureCilium(),
		//steps.EnableAndStartCilium(),
		//steps.HelmInstallMetallb(),
		//steps.DeployMetallbConfig(),
		////steps.DeployMetricsServer(),
		////steps.DeployCertManager(),
		//steps.CheckClusterHealth(),
	)
}
