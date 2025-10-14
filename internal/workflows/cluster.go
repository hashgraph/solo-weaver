package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
)

func NewSetupClusterWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubernetes").
		Steps(
			// preflight & basic setup
			NewNodeSetupWorkflow(nodeType),

			// setup env for k8s
			steps.DisableSwap(),
			steps.InstallKernelModules(),
			steps.ConfigureSysctlForKubernetes(),
			steps.SetupBindMounts(),

			// kubelet
			steps.SetupKubelet(),
			steps.EnableAndStartKubelet(),

			// setup cli tools
			steps.SetupKubectl(),
			steps.SetupHelm(), // required by MetalLB setup, so we install it earlier
			steps.SetupK9s(),

			// CRI-O
			steps.SetupCrio2(),
			steps.EnableAndStartCrio2(),

			// kubeadm
			steps.SetupKubeadm(),

			// init cluster
			steps.InitCluster(),

			// cilium CNI
			steps.SetupCiliumCNI(),
			steps.EnableAndStartCiliumCNI(),

			// metalLB
			steps.SetupMetalLB(),

			// health check
			steps.CheckClusterHealth(),
		)
}
