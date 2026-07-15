// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/config"
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
//
// It is workload-agnostic: it validates only the Kubernetes substrate hardware floor
// (what Kubernetes itself needs), not any per-workload sizing. Per-workload preflight
// belongs to the node install commands (e.g. block node install), which run
// NodeSetupWorkflow before composing KubernetesSetupWorkflow themselves.
func InstallClusterWorkflow(skipHardwareChecks bool, mr software.MachineRuntime) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(
			SubstrateSetupWorkflow(skipHardwareChecks),
			KubernetesSetupWorkflow(mr),
		)
}

// KubernetesSetupWorkflow installs and configures Kubernetes components.
// Rendered as the "Kubernetes Setup" phase in the TUI. It is node-type-agnostic and is
// composed both by InstallClusterWorkflow (cluster install) and by node install handlers
// (e.g. block node install) after their own workload preflight + system setup.
func KubernetesSetupWorkflow(mr software.MachineRuntime) *automa.WorkflowBuilder {
	wfSteps := []automa.Builder{
		// setup env for k8s
		steps.DisableSwap(),
		steps.ConfigureSysctlForKubernetes(),
		steps.SetupBindMounts(),

		// kubelet
		steps.SetupKubelet(mr),
		steps.VerifyExecutablesStep("kubelet"),
		steps.SetupSystemdService(software.KubeletServiceName),

		// setup cli tools
		steps.SetupKubectl(mr),
		steps.SetupHelm(mr), // required by MetalLB setup, so we install it earlier
		steps.SetupK9s(mr),

		// CRI-O
		steps.SetupCrio(mr),
		steps.VerifyExecutablesStep(software.CrioArtifactName),
		steps.SetupSystemdService(software.CrioServiceName),

		// kubeadm
		steps.SetupKubeadm(mr),

		// init cluster
		steps.InitializeCluster(),

		steps.SetupCilium(mr),
		steps.StartCilium(),

		steps.SetupMetalLB(),

		steps.DeployMetricsServer(nil),
	}

	if config.Get().SoloOperator.Enabled {
		wfSteps = append(wfSteps, steps.InstallSoloOperator())
	}

	wfSteps = append(wfSteps, steps.CheckClusterHealth())

	return automa.NewWorkflowBuilder().
		WithId("kubernetes-setup").
		Steps(wfSteps...).
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
