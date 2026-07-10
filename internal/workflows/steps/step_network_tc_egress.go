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

// TcEgressPersistStepId is the step ID for TcEgressPersist.
const TcEgressPersistStepId = "tc-egress-persist"

// TcEgressPersist renders /usr/local/sbin/solo-provisioner-tc-egress.sh with
// the egress NIC name interpolated, installs and enables the
// solo-provisioner-tc-egress.service oneshot unit, and executes the script
// immediately so the kernel HTB hierarchy is live without waiting for a reboot
// (design §8.3.2).
//
// When nicName is empty the NIC is auto-detected from the default route via
// DetectEgressInterface (single-NIC hosts, §4.1). Pass --egress-interface to
// override on multi-NIC hosts or when the default route does not identify the
// correct physical interface.
func TcEgressPersist(nicName string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(TcEgressPersistStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Persisting tc-egress HTB hierarchy for reboot")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to persist tc-egress hierarchy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "tc-egress hierarchy persisted")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			nic := nicName
			if nic == "" {
				detected, err := shape.DetectEgressInterface()
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to auto-detect egress interface").
							WithProperty(models.ErrPropertyResolution, []string{
								"Specify the physical NIC explicitly: block node install --egress-interface <nic>",
								"To find the correct NIC: ip route get 8.8.8.8 (look for 'dev <nic>')",
							})))
				}
				logx.As().Info().Str("nic", detected).Msg("auto-detected egress interface from default route")
				nic = detected
			}

			if err := shape.RenderTcEgressScript(nic); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			if err := shape.EnsureTcEgressUnit(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			if err := shape.ApplyTcEgressScript(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: the script and unit are idempotent artifacts.
			// Removing them would leave the kernel hierarchy installed but un-
			// persisted — no worse than before this step ran. A dedicated
			// `block node uninstall` step (Story 2.4 / #763) handles teardown.
			return automa.SkippedReport(stp,
				automa.WithDetail("tc-egress rollback is a no-op; teardown is handled by block node uninstall (#763)"))
		})
}
