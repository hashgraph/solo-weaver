// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// UpgradeCiliumCLIWorkflow puts the cilium CLI on version.
func UpgradeCiliumCLIWorkflow(version string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("upgrade-cilium-cli").
		Steps(steps.UpgradeHostBinary("cilium", version, true, software.NewCiliumInstaller))
}

// CiliumHopWorkflow moves the running Cilium from one catalog version to the
// next. It stops at the first failed step and has no rollback: going back is a
// second `cilium upgrade`, which a person should decide on.
func CiliumHopWorkflow(from, to string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("cilium-hop").
		Steps(steps.CiliumHopSteps(from, to, kube.NewClient)...)
}

// KubernetesHopWorkflow moves a single-node cluster from one Kubernetes
// version to the next, with cri-o at crio. It stops at the first failed step
// and has no rollback, since Kubernetes cannot be downgraded; a failure starts
// cri-o and the kubelet again and lifts the hop's cordon.
func KubernetesHopWorkflow(from, to, crio string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("kubernetes-hop").
		WithExecutionMode(automa.StopOnError).
		Steps(steps.KubernetesHopSteps(from, to, crio, kube.NewClient)...).
		WithOnFailure(func(ctx context.Context, _ automa.Step, _ *automa.Report) {
			steps.RecoverKubernetesHop(ctx, kube.NewClient)
		})
}
