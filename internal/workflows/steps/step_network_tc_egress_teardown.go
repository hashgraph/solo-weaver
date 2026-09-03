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

// TcEgressTeardownStepId is the step ID for TcEgressTeardown.
const TcEgressTeardownStepId = "bandwidth-shaper-teardown"

// TcEgressTeardown removes the egress tc HTB hierarchy (device root + all egress
// classes), dropping the live shaping on the physical NIC.
//
// unitWillBeRemoved distinguishes its two callers. `block node reconfigure
// --traffic-shaping-enabled=false` (network_setup.go) passes false: it is the
// disable counterpart to TcEgressPersist, and re-renders the boot script to its
// empty default so the unit — which stays installed — doesn't resurrect the old
// shape on reboot. `block node uninstall` (uninstall_handler.go) passes true: it
// runs TcEgressServiceTeardown right after, which deletes that same unit and boot
// script, so re-rendering the script and reconciling the unit here would be
// wasted work — and an unnecessary systemd/D-Bus dependency on a path that has
// nothing left to reconcile.
//
// It must run after NetworkPolicyDeleteAll: the policy plane's --stamp rules
// reference these classes, and the underlying shape teardown assumes those
// references are already gone. The teardown is idempotent — with no egress config
// present it succeeds without shaping anything.
func TcEgressTeardown(unitWillBeRemoved bool) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(TcEgressTeardownStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing bandwidth-shaper HTB hierarchy")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove bandwidth-shaper hierarchy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "bandwidth-shaper hierarchy removed")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := shape.NewManager().TeardownEgress(ctx, unitWillBeRemoved); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to tear down the bandwidth-shaper HTB hierarchy").
						WithProperty(models.ErrPropertyResolution, []string{
							"Check the bandwidth-shaper service journal: journalctl -u solo-provisioner-bandwidth-shaper.service -n 20",
							"Inspect the live qdisc: tc qdisc show",
							"Verify the egress NIC is detectable: ip route get 8.8.8.8 | grep dev",
						})))
			}
			logx.As().Info().Msg("bandwidth-shaper HTB hierarchy removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: the shaped hierarchy was provisioned from the
			// operator's link-rate/--shape inputs, which this teardown step does not
			// snapshot. Re-enabling shaping is an explicit reconfigure decision.
			return automa.SkippedReport(stp,
				automa.WithDetail("bandwidth-shaper teardown rollback is a no-op; re-enable via block node reconfigure --traffic-shaping-enabled"))
		})
}
