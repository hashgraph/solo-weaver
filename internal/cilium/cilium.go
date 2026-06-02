// SPDX-License-Identifier: Apache-2.0

// Package cilium provides cluster-level operations against the running Cilium
// agent DaemonSet (installed by weaver via internal/templates/files/cilium/).
//
// Cluster bootstrap (host-level `cilium install` via the cilium CLI binary) lives
// in internal/workflows/steps/step_cilium.go and is separate from these helpers,
// which talk to the running DaemonSet through the kube API.
package cilium

import (
	"context"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/joomcode/errorx"
)

const (
	// AgentDaemonSetNamespace and AgentDaemonSetName identify the Cilium agent
	// DaemonSet that weaver installs.
	AgentDaemonSetNamespace = "kube-system"
	AgentDaemonSetName      = "cilium"

	// DefaultRolloutTimeout is the conservative deadline for waiting on a Cilium
	// DaemonSet rolling restart on a small cluster (~30s on a single-node cluster).
	DefaultRolloutTimeout = 90 * time.Second
)

// RestartAgentDaemonSet triggers a rolling restart of the Cilium agent DaemonSet
// and blocks until the rollout is complete (or timeout elapses).
//
// This is the documented workaround for the eBPF service reconciler missing
// Service.spec.type mutations (see hashgraph/solo-weaver issue #619). eBPF
// programs stay loaded across the restart, so established connections survive;
// only the agent control plane bounces.
func RestartAgentDaemonSet(ctx context.Context, kubeClient *kube.Client, timeout time.Duration) error {
	if err := kubeClient.RolloutRestartDaemonSet(ctx, AgentDaemonSetNamespace, AgentDaemonSetName); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to trigger Cilium DaemonSet rollout restart")
	}
	if err := kubeClient.WaitForResource(
		ctx,
		kube.KindDaemonSet,
		AgentDaemonSetNamespace,
		AgentDaemonSetName,
		kube.IsDaemonSetRolledOut,
		timeout,
	); err != nil {
		return errorx.IllegalState.Wrap(err, "Cilium DaemonSet did not finish rolling out within %s", timeout)
	}
	return nil
}
