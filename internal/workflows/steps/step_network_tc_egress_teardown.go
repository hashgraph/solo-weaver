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

// TcEgressTeardownStepId is the step ID for TcEgressTeardown and TcEgressDisable.
// They present as the same step to the operator ("Removing bandwidth-shaper HTB
// hierarchy"); only their manager-level implementation differs.
const TcEgressTeardownStepId = "bandwidth-shaper-teardown"

// TcEgressTeardown is the narrower of this pair — it only drops the live shaping
// (device root + all egress classes) on the physical NIC. Wired into `block node
// uninstall` (uninstall_handler.go), immediately followed by TcEgressServiceTeardown,
// which deletes the bandwidth-shaper unit and boot script — so this step never
// reconciles or restarts that unit; there would be nothing left to keep in sync.
//
// It must run after NetworkPolicyDeleteAll: the policy plane's --stamp rules
// reference these classes, and the underlying shape teardown assumes those
// references are already gone. The teardown is idempotent — with no egress config
// present it succeeds without shaping anything.
func TcEgressTeardown() *automa.StepBuilder {
	return newTcEgressTeardownStep(shape.NewManager().TeardownEgress)
}

// TcEgressDisable is the fuller counterpart: `block node reconfigure
// --traffic-shaping-enabled=false` (network_setup.go) has no follow-up step, so
// unlike TcEgressTeardown it also re-renders the boot script to its empty default
// and reconciles+restarts the bandwidth-shaper unit over D-Bus, so a reboot
// doesn't resurrect the old shape. It is the disable counterpart to TcEgressPersist.
func TcEgressDisable() *automa.StepBuilder {
	return newTcEgressTeardownStep(shape.NewManager().DisableEgress)
}

// newTcEgressTeardownStep builds the shared step scaffolding for TcEgressTeardown
// and TcEgressDisable; exec is the manager call that differs between them.
func newTcEgressTeardownStep(exec func(ctx context.Context) error) *automa.StepBuilder {
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
			if err := exec(ctx); err != nil {
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
