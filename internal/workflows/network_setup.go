// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NetworkSetupWorkflow lays down the block node's static network plane: the
// node-level host firewall (inet host), the weaver policy-table persistence
// (inet weaver), and the $EGRESS / $VETH tc HTB shape config. It is rendered as
// the "Network Setup" phase so these steps group under their own header in the
// TUI instead of dangling as loose sub-steps after the "Kubernetes Setup" phase.
//
// Ordering note preserved from the caller: this must run after the Kubernetes
// setup, since nftables is installed/enabled by the system-setup phase before the
// host firewall is applied here.
func NetworkSetupWorkflow(egressInterface, linkRate string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("block-node-network-setup").
		Steps(
			steps.NetworkFirewallCreate(),
			steps.NftWeaverPersist(),
			steps.TcEgressPersist(egressInterface, linkRate),
			steps.TcIngressRecord(egressInterface, linkRate),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Network Setup")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Network Setup")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Network Setup")
		})
}

// BlockNodeDaemonConfigWorkflow enables the block-node traffic-shaper monitor in
// daemon.yaml. It wraps the single config step in a "Traffic-shaper Monitor" phase
// so it renders under its own header rather than dangling after the "Block Node
// Deployment" phase. It runs last, after the deployment it watches is in place.
func BlockNodeDaemonConfigWorkflow(namespace string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("block-node-daemon-config").
		Steps(
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), namespace),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Traffic-shaper Monitor")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Traffic-shaper Monitor")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Traffic-shaper Monitor")
		})
}
