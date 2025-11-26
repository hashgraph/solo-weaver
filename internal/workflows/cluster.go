package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/workflows/steps"
	"golang.hedera.com/solo-weaver/pkg/software"
)

func NewSetupClusterWorkflow() *automa.WorkflowBuilder {
	// Build the base steps that are common to all node types
	baseSteps := []automa.Builder{
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
		steps.InitializeCluster(),

		// cilium
		steps.SetupCilium(),
		steps.StartCilium(),

		// metalLB
		steps.SetupMetalLB(),

		// health check
		steps.CheckClusterHealth(), // still using bash steps
	}

	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(baseSteps...)
}
