package steps

import (
	"github.com/automa-saga/automa"
)

func SetupKubeadm() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubeadm").Steps(
		bashSteps.DownloadKubeadm(),
		bashSteps.InstallKubeadm(),
		bashSteps.TorchPriorKubeAdmConfiguration(),
		bashSteps.DownloadKubeadmConfig(),
		bashSteps.ConfigureKubeadm(), // needed for kubelet
	)
}

func InitCluster() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("init-cluster").Steps(
		bashSteps.InitCluster(),
		bashSteps.ConfigureKubeConfig(),
	)
}
