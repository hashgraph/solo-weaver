// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// CiliumHopWorkflow moves the running Cilium from one catalog version to the
// next with the cilium CLI at cli, which it first installs when the host has
// another version. It stops at the first failed step and has no rollback:
// going back is a second `cilium upgrade`, which a person should decide on.
func CiliumHopWorkflow(from, to, cli string) *automa.WorkflowBuilder {
	all := []automa.Builder{steps.UpgradeHostBinary(software.CiliumBinaryName, cli, true, software.NewCiliumInstaller)}
	all = append(all, steps.CiliumHopSteps(from, to, cli, kube.ClientFromContext)...)
	return automa.NewWorkflowBuilder().WithId("cilium-hop").Steps(all...)
}

// KubernetesHopWorkflow moves a single-node cluster from one Kubernetes
// version to the next, with cri-o at crio. It stops at the first failed step
// and has no rollback, since Kubernetes cannot be downgraded; a failure starts
// cri-o and the kubelet again and lifts the hop's cordon.
func KubernetesHopWorkflow(from, to, crio string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("kubernetes-hop").
		WithExecutionMode(automa.StopOnError).
		Steps(steps.KubernetesHopSteps(from, to, crio, kube.ClientFromContext)...).
		WithOnFailure(func(ctx context.Context, _ automa.Step, _ *automa.Report) {
			steps.RecoverKubernetesHop(ctx, kube.ClientFromContext)
		})
}

// NodeExporterReplaceWorkflow replaces the Bitnami node-exporter with the
// prometheus-community chart. It stops at the first failed step and has no
// rollback.
func NodeExporterReplaceWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("node-exporter-replace").
		WithExecutionMode(automa.StopOnError).
		Steps(steps.NodeExporterReplaceSteps(kube.ClientFromContext)...)
}

// ExternalSecretsMigrateWorkflow moves External Secrets from a 0.x chart to to.
// It stops at the first failed step and has no rollback.
func ExternalSecretsMigrateWorkflow(to string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("external-secrets-migrate").
		WithExecutionMode(automa.StopOnError).
		Steps(steps.ExternalSecretsMigrateSteps(to, kube.ClientFromContext)...)
}

// ChartHopWorkflow upgrades the charts of one charts hop. It stops at the
// first failed chart and has no rollback.
func ChartHopWorkflow(bumps []steps.ChartBump) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("chart-hop").
		WithExecutionMode(automa.StopOnError).
		Steps(steps.ChartHopSteps(bumps, kube.ClientFromContext)...)
}
