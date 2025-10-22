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
			steps.SetupBindMounts(), // still using bash steps

			// kubelet
			steps.SetupKubelet(),
			steps.SetupSystemdService("kubelet"),

			// setup cli tools
			steps.SetupKubectl(),
			steps.SetupHelm(), // required by MetalLB setup, so we install it earlier
			steps.SetupK9s(),

			// CRI-O
			steps.SetupCrio2(), // still using bash steps
			steps.SetupSystemdService("crio"),

			// kubeadm
			steps.SetupKubeadm(),

			// init cluster
			steps.InitCluster(),

			// cilium CLI
			steps.SetupCiliumCLI(),

			// cilium CNI
			steps.SetupCiliumCNI(),          // still using bash steps
			steps.EnableAndStartCiliumCNI(), // still using bash steps

			// metalLB
			steps.SetupMetalLB(), // still using bash steps

			// health check
			steps.CheckClusterHealth(), // still using bash steps
		)
}
