package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func NewSetupClusterWorkflow(nodeType string) automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubernetes").
		Steps(
			// preflight & basic setup
			NewNodeSetupWorkflow(nodeType),

			// setup env for k8s
			steps.DisableSwap(),
			steps.ConfigureSysctlForKubernetes(),
			steps.SetupBindMounts(),

			// kubelet
			steps.SetupKubelet(),
			steps.SetupSystemdService(software.KubeletServiceName),

			// setup cli tools
			steps.SetupKubectl(),
			steps.SetupHelm(), // required by MetalLB setup, so we install it earlier
			steps.SetupK9s(),

			// CRI-O
			steps.SetupCrio(),
			steps.SetupSystemdService(software.CrioServiceName),

			// kubeadm
			steps.SetupKubeadm(),

			// init cluster
			steps.InitCluster(),

			// cilium CLI
			steps.SetupCilium(),

			// metalLB
			steps.SetupMetalLB(), // still using bash steps

			// health check
			steps.CheckClusterHealth(), // still using bash steps
		)
}
