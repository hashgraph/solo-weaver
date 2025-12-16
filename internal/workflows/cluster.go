// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"helm.sh/helm/v3/pkg/cli/values"
)

// ClusterSetupOptions defines options for setting up the cluster
type ClusterSetupOptions struct {
	EnableMetricsServer bool
}

// NewSetupClusterWorkflow creates a workflow to set up a kubernetes cluster
func NewSetupClusterWorkflow(opts ClusterSetupOptions) *automa.WorkflowBuilder {
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
	}

	if opts.EnableMetricsServer {
		metricsValues := &values.Options{
			Values: []string{
				"apiService.insecureSkipTLSVerify=false",
				"tls.type=helm",
			},
		}
		baseSteps = append(baseSteps, steps.DeployMetricsServer(metricsValues))
	}

	// health check
	baseSteps = append(baseSteps, steps.CheckClusterHealth())

	return automa.NewWorkflowBuilder().
		WithId("setup-kubernetes").
		Steps(baseSteps...)
}
