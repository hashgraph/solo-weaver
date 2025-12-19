// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// ClusterSetupOptions defines options for setting up various components of the cluster
type ClusterSetupOptions struct {
	SetupCilium        bool
	SetupMetalLB       bool
	SetupMetricsServer bool
	CheckClusterHealth bool
	ExecutionMode      automa.TypeMode
	RollbackMode       automa.TypeMode
}

// DefaultClusterSetupOptions returns ClusterSetupOptions with all boolean options defaulted to true.
func DefaultClusterSetupOptions() *ClusterSetupOptions {
	return &ClusterSetupOptions{
		SetupCilium:        true,
		SetupMetalLB:       true,
		SetupMetricsServer: true,
		CheckClusterHealth: true,
		ExecutionMode:      automa.StopOnError,
	}
}

// NewSetupClusterWorkflow creates a workflow to set up a kubernetes cluster
func NewSetupClusterWorkflow(opts *ClusterSetupOptions) *automa.WorkflowBuilder {
	if opts == nil {
		opts = DefaultClusterSetupOptions()
	}

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
	}

	if opts.SetupCilium {
		baseSteps = append(baseSteps, steps.SetupCilium(), steps.StartCilium())
	}

	if opts.SetupMetalLB {
		baseSteps = append(baseSteps, steps.SetupMetalLB())
	}

	if opts.SetupMetricsServer {
		baseSteps = append(baseSteps, steps.DeployMetricsServer(nil))
	}

	// health check
	if opts.CheckClusterHealth {
		baseSteps = append(baseSteps, steps.CheckClusterHealth())
	}

	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(baseSteps...).
		WithExecutionMode(opts.ExecutionMode)
}
