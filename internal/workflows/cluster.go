// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
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
func InstallClusterWorkflow(nodeType string, profile string, skipHardwareChecks bool, mr software.MachineRuntime) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(
			NodeSetupWorkflow(nodeType, profile, skipHardwareChecks),
			kubernetesSetupWorkflow(mr),
		)
}

// kubernetesSetupWorkflow installs and configures Kubernetes components.
// Rendered as the "Kubernetes Setup" phase in the TUI.
func kubernetesSetupWorkflow(mr software.MachineRuntime) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("kubernetes-setup").
		Steps(
			// setup env for k8s
			steps.DisableSwap(),
			steps.ConfigureSysctlForKubernetes(),
			steps.SetupBindMounts(),

			// kubelet
			steps.SetupKubelet(mr),
			steps.SetupSystemdService(software.KubeletServiceName),

			// setup cli tools
			steps.SetupKubectl(mr),
			steps.SetupHelm(mr), // required by MetalLB setup, so we install it earlier
			steps.SetupK9s(mr),

			// CRI-O
			steps.SetupCrio(mr),
			steps.SetupSystemdService(software.CrioServiceName),

			// kubeadm
			steps.SetupKubeadm(mr),

			// init cluster
			steps.InitializeCluster(),

			steps.SetupCilium(mr),
			steps.StartCilium(),

			steps.SetupMetalLB(),

			steps.DeployMetricsServer(nil),
			steps.CheckClusterHealth(),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Kubernetes Setup")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Kubernetes Setup")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Kubernetes Setup")
		})
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
