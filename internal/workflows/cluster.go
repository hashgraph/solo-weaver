// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// DefaultWorkflowExecutionOptions returns WorkflowExecutionOptions with all boolean options defaulted to true.
func DefaultWorkflowExecutionOptions() *models.WorkflowExecutionOptions {
	return &models.WorkflowExecutionOptions{
		ExecutionMode: automa.StopOnError,
		RollbackMode:  automa.ContinueOnError,
	}
}

// InstallClusterWorkflow creates a workflow to set up a kubernetes cluster.
func InstallClusterWorkflow(nodeType string, profile string, skipHardwareChecks bool, sm state.Manager) *automa.WorkflowBuilder {
	// Build the base steps that are common to all node types
	baseSteps := []automa.Builder{
		NodeSetupWorkflow(nodeType, profile, skipHardwareChecks),

		// setup env for k8s
		steps.DisableSwap(),
		steps.ConfigureSysctlForKubernetes(),
		steps.SetupBindMounts(),

		// kubelet
		steps.SetupKubelet(sm),
		steps.SetupSystemdService(software.KubeletServiceName),

		// setup cli tools
		steps.SetupKubectl(sm),
		steps.SetupHelm(sm), // required by MetalLB setup, so we install it earlier
		steps.SetupK9s(sm),

		// CRI-O
		steps.SetupCrio(sm),
		steps.SetupSystemdService(software.CrioServiceName),

		// kubeadm
		steps.SetupKubeadm(sm),

		// init cluster
		steps.InitializeCluster(),

		steps.SetupCilium(sm),
		steps.StartCilium(),

		steps.SetupMetalLB(),

		steps.DeployMetricsServer(nil),
		steps.CheckClusterHealth(),
	}

	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(baseSteps...)
}

// WithWorkflowExecutionMode applies the given WorkflowExecutionOptions to the provided WorkflowBuilder.
// If opts is nil, it uses DefaultWorkflowExecutionOptions.
func WithWorkflowExecutionMode(wf *automa.WorkflowBuilder, opts *models.WorkflowExecutionOptions) *automa.WorkflowBuilder {
	if opts == nil {
		opts = DefaultWorkflowExecutionOptions()
	}

	return wf.WithExecutionMode(opts.ExecutionMode).WithRollbackMode(opts.RollbackMode)
}

// UninstallClusterWorkflow creates a workflow to tear down a kubernetes cluster
func UninstallClusterWorkflow() *automa.WorkflowBuilder {
	teardownSteps := []automa.Builder{
		// Reset the Kubernetes cluster
		steps.ResetCluster(),

		// Stop services
		steps.TeardownSystemdService(software.CrioServiceName),
		steps.TeardownSystemdService(software.KubeletServiceName),

		// Remove bind mounts
		steps.TeardownBindMounts(),

		// Remove systemd service files
		steps.RemoveSystemdServiceFiles(),

		// Remove configuration directories
		steps.RemoveConfigDirectories(),

		// Cleanup weaver files (preserving downloads)
		steps.CleanupWeaverFiles(),
	}

	return automa.NewWorkflowBuilder().
		WithId("teardown-kubernetes").
		Steps(teardownSteps...)
}
