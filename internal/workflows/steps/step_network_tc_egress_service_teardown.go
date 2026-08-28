// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// TcEgressServiceTeardownStepId is the step ID for TcEgressServiceTeardown.
const TcEgressServiceTeardownStepId = "bandwidth-shaper-service-teardown"

// TcEgressServiceTeardown stops, disables and removes the
// solo-provisioner-bandwidth-shaper.service unit along with the boot script it
// executes. Unlike the shared network-nft unit — which replays the host firewall
// too and so outlives the block node — this unit exists only to replay the
// $EGRESS HTB hierarchy that block node install lays down, so it is removed here
// rather than deferred to kube cluster uninstall.
//
// It must run after TcEgressTeardown, which drops the live hierarchy and resets
// the boot script; this step then removes the boot-replay machinery itself. The
// step is idempotent: an already-absent unit or script is not an error.
func TcEgressServiceTeardown() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(TcEgressServiceTeardownStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing bandwidth-shaper boot service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove bandwidth-shaper boot service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "bandwidth-shaper boot service removed")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := shape.RemoveTcEgressUnit(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to remove the bandwidth-shaper boot service").
						WithProperty(models.ErrPropertyResolution, []string{
							// Script first, mirroring RemoveTcEgressUnit's removal order.
							"Remove the boot script: rm " + shape.TcEgressScriptPath,
							"Disable manually: systemctl disable " + shape.TcEgressService,
							"Remove the unit file: rm " + shape.TcEgressServiceUnitPath,
							"Then reload systemd: systemctl daemon-reload",
						})))
			}
			logx.As().Info().Str("service", shape.TcEgressService).
				Msg("bandwidth-shaper boot service removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SkippedReport(stp,
				automa.WithDetail("bandwidth-shaper service teardown rollback is a no-op; re-enable via block node install"))
		})
}
