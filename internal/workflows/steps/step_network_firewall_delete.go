// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// NetworkFirewallDeleteStepId is the step ID for NetworkFirewallDelete.
const NetworkFirewallDeleteStepId = "network-firewall-delete"

// NetworkFirewallDelete removes the node-level `inet weaver-host-firewall` nftables table (the
// same teardown as `network firewall delete`). It is the disable counterpart to
// NetworkFirewallCreate, wired into `block node reconfigure` when the operator
// turns the host firewall off on an already-provisioned host.
//
// The delete is idempotent (firewall.Manager.Delete existence-checks before
// removing), so running it when no table is present is a no-op. It deliberately
// does NOT disable the shared solo-provisioner-network-nft.service — that unit is
// also used by the `inet weaver-workload-policy` plane and is only torn down by a full cluster
// uninstall.
func NetworkFirewallDelete() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NetworkFirewallDeleteStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing host firewall (inet weaver-host-firewall)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove host firewall")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Host firewall removed")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := newFirewallManager().Delete(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to remove the host firewall (inet weaver-host-firewall) table").
						WithProperty(models.ErrPropertyResolution, []string{
							"Inspect the live table: nft list table inet weaver-host-firewall",
							"Remove it manually if needed: nft delete table inet weaver-host-firewall",
							"Check the persisted artifact: ls -la " + firewall.HostNftPath,
						})))
			}
			logx.As().Info().Msg("host firewall (inet weaver-host-firewall) table removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: the table was rendered from the resolved host
			// config, which this step does not snapshot. Re-enabling the firewall is
			// an explicit operator decision (block node reconfigure --firewall-enabled),
			// not something to silently reconstruct on rollback.
			return automa.SkippedReport(stp,
				automa.WithDetail("host firewall delete rollback is a no-op; re-enable via block node reconfigure --firewall-enabled"))
		})
}
