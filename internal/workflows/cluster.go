package workflows

import (
	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/workflows/steps"
	"golang.hedera.com/solo-weaver/pkg/software"
)

func NewSetupClusterWorkflow(nodeType string, profile string) automa.Builder {
	// Build the base steps that are common to all node types
	baseSteps := []automa.Builder{
		// preflight & basic setup
		NewNodeSetupWorkflow(nodeType, profile),

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

	// Add block node setup if this is a block node
	if nodeType == core.NodeTypeBlock {
		baseSteps = append(baseSteps, steps.SetupBlockNode(nodeType, profile))
	}

	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(baseSteps...)
}
