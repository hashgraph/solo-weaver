// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// TcIngressRecordStepId is the step ID for TcIngressRecord.
const TcIngressRecordStepId = "tc-ingress-record"

// TcIngressRecord records the ingress ($VETH) HTB shape config so the daemon
// pod-lifecycle watcher can replay it on each block-node pod create. It writes
// the ingress device root and the three default classes (publisher/backfill-
// response/reserve-ingress) at proportional rates into the shape registry, but —
// unlike TcEgressPersist — renders NO boot script: the $VETH qdisc is ephemeral
// (Cilium recreates the veth per pod) and is deliberately not persisted across
// reboot.
//
// Ingress bandwidth defaults to egress: linkRate is the operator's --link-rate
// (the $EGRESS trunk). When empty it resolves "auto" — the NIC's detected link
// speed at install time — so the recorded class rates are always concrete, since
// the per-pod replay has no sysfs fallback. nicName pins auto-resolution to the
// operator-chosen NIC on multi-NIC hosts.
func TcIngressRecord(nicName string, linkRate string, overrides map[string]shape.ClassOverride) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(TcIngressRecordStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Recording tc-ingress ($VETH) HTB shape for per-pod replay")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to record tc-ingress shape")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "tc-ingress shape recorded")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			rate := linkRate
			if rate == "" {
				rate = "auto"
			}
			if err := shape.ProvisionDefaultIngressShape(ctx, nicName, rate, overrides); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to record default ingress shape").
						WithProperty(models.ErrPropertyResolution, []string{
							"Inspect the recorded ingress config: ls /etc/solo-provisioner/network/shape/devices /etc/solo-provisioner/network/shape/classes",
							"Verify --link-rate is a valid tc-style rate (e.g. 1gbit, 100mbit) or \"auto\"",
							"Re-run the install: block node install --link-rate <rate>",
						})))
			}
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: the recorded config is an idempotent artifact and
			// is not applied to the kernel here (the daemon replays it per pod).
			// Teardown is handled by block node uninstall.
			return automa.SkippedReport(stp,
				automa.WithDetail("tc-ingress rollback is a no-op; teardown is handled by block node uninstall"))
		})
}
