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
const TcEgressPersistStepId = "bandwidth-shaper-persist"

// TcEgressPersist provisions the egress tc HTB hierarchy for reboot persistence.
// It writes the egress device root and three default classes
// (partner/public/reserve-egress) into the shape registry at proportions derived
// from trunkRate with any per-class overrides merged in, then renders and
// applies the boot script. An empty trunkRate resolves to "auto" — the link
// speed detected at install time — so the recorded rates are always concrete.
// This mirrors TcIngressRecord's unconditional provisioning so `network shape
// show` always reports all six classes after install, not just the three from
// TcIngressRecord.
//
// Re-running this step (reconfigure/upgrade) does not clobber operator-applied
// `network shape set` adjustments: when the resolved trunk rate is the same
// bandwidth already recorded on the device, the shape layer keeps each class's
// recorded rate/ceil/prio and merges only this run's overrides on top (see
// shape.mergeExistingConfig). A trunk rate that actually changed rebalances
// the classes proportionally — the intentional --link-rate path.
//
// When nicName is empty the NIC is auto-detected from the default route via
// DetectEgressInterface. Pass --egress-interface to override on multi-NIC
// hosts or when the default route does not identify the correct physical
// interface.
func TcEgressPersist(nicName string, trunkRate string, overrides map[string]shape.ClassOverride) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(TcEgressPersistStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Persisting bandwidth-shaper HTB hierarchy for reboot")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to persist bandwidth-shaper hierarchy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "bandwidth-shaper hierarchy persisted")
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
								"To find the correct NIC: ip route get 8.8.8.8 | grep dev",
							})))
				}
				logx.As().Info().Str("nic", detected).Msg("auto-detected egress interface from default route")
				nic = detected
			}

			tcEgressResolution := []string{
				"Check the bandwidth-shaper service journal for the actual tc error: journalctl -u solo-provisioner-bandwidth-shaper.service -n 20",
				"Verify the NIC name exists on this host: ip link show",
				"If the NIC is wrong, specify the correct one: block node install --egress-interface <nic>",
				"Find the NIC used by the default route: ip route get 8.8.8.8 | grep dev",
			}

			// Resolve an empty rate to "auto" so the proportional defaults are
			// computed against the detected link speed at install time. On a
			// re-run the shape layer folds the recorded class values back in, so
			// this single call covers both the fresh install and the
			// reconfigure/upgrade re-run without needing to guess which one it is
			// from the emptiness of trunkRate — a signal reconfigure/upgrade
			// cannot provide, since both resolve the rate back from state (#1037).
			rate := trunkRate
			if rate == "" {
				rate = "auto"
			}
			if err := shape.ProvisionDefaultEgressShape(ctx, nic, rate, overrides); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to provision default egress shape").
						WithProperty(models.ErrPropertyResolution, tcEgressResolution)))
			}
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: the script and unit are idempotent artifacts.
			// Removing them would leave the kernel hierarchy installed but un-
			// persisted — no worse than before this step ran. Teardown is handled
			// by block node uninstall.
			return automa.SkippedReport(stp,
				automa.WithDetail("bandwidth-shaper rollback is a no-op; teardown is handled by block node uninstall"))
		})
}
