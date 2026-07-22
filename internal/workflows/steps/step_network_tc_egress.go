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

// TcEgressPersist provisions the egress tc HTB hierarchy for reboot persistence.
// It writes the egress device root and three default classes
// (partner/public/reserve-egress) into the shape registry — either at the
// operator-supplied trunkRate/overrides, or, when neither was supplied and no
// egress registry entry exists yet (a fresh install), at auto-detected
// defaults. This mirrors TcIngressRecord's unconditional provisioning so
// `network shape show` always reports all six classes after install, not just
// the three from TcIngressRecord.
//
// When trunkRate/overrides are empty and an egress registry entry already
// exists (a reconfigure/upgrade re-run that didn't pass --link-rate/--shape),
// it re-renders the boot script from that existing config instead, so it
// never clobbers operator-applied `network shape set` adjustments.
//
// When nicName is empty the NIC is auto-detected from the default route via
// DetectEgressInterface. Pass --egress-interface to override on multi-NIC
// hosts or when the default route does not identify the correct physical
// interface.
func TcEgressPersist(nicName string, trunkRate string, overrides map[string]shape.ClassOverride) *automa.StepBuilder {
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
								"To find the correct NIC: ip route get 8.8.8.8 | grep dev",
							})))
				}
				logx.As().Info().Str("nic", detected).Msg("auto-detected egress interface from default route")
				nic = detected
			}

			tcEgressResolution := []string{
				"Check the tc-egress service journal for the actual tc error: journalctl -u solo-provisioner-tc-egress.service -n 20",
				"Verify the NIC name exists on this host: ip link show",
				"If the NIC is wrong, specify the correct one: block node install --egress-interface <nic>",
				"Find the NIC used by the default route: ip route get 8.8.8.8 | grep dev",
			}

			hasExisting, err := shape.NewManager().HasEgressConfig()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to check existing egress shape config").
						WithProperty(models.ErrPropertyResolution, tcEgressResolution)))
			}

			// Provision explicit class config when the operator gave a trunk rate,
			// gave per-class --shape overrides (which need concrete classes to
			// merge into), or there is no egress registry entry yet (a fresh
			// install — matches TcIngressRecord's unconditional provisioning, so
			// the registry always ends up populated with all six classes).
			// Resolve an empty rate to "auto" so the proportional defaults are
			// computed against the detected link speed at install time.
			if trunkRate != "" || len(overrides) > 0 || !hasExisting {
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
			}

			// No trunk rate or overrides supplied, and a registry entry already
			// exists (reconfigure/upgrade without --link-rate/--shape): re-render
			// from that existing shape config, then apply.
			if err := shape.RenderAndApplyDefaultEgress(ctx, nic); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to apply tc-egress script").
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
				automa.WithDetail("tc-egress rollback is a no-op; teardown is handled by block node uninstall"))
		})
}
