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

// TcIngressTeardownStepId is the step ID for TcIngressTeardown.
const TcIngressTeardownStepId = "tc-ingress-teardown"

// TcIngressTeardown removes the ingress tc shape config (device root + all
// ingress classes) from the shape registry. There is no boot script to
// re-render: the $VETH HTB is ephemeral (Cilium recreates the veth on each
// pod create) and was deliberately not persisted for boot replay. It is the
// teardown counterpart to TcIngressRecord, wired into `block node uninstall`.
//
// It must run after NetworkPolicyDeleteAll: the policy plane's --reply-stamp
// rules reference these classes, and the underlying shape teardown assumes
// those references are already gone. The teardown is idempotent — with no
// ingress config present it succeeds immediately.
func TcIngressTeardown() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(TcIngressTeardownStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing tc-ingress ($VETH) shape config")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove tc-ingress shape config")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "tc-ingress shape config removed")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := shape.NewManager().TeardownIngress(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to tear down the tc-ingress shape config").
						WithProperty(models.ErrPropertyResolution, []string{
							"Inspect the recorded ingress config: ls /etc/solo-provisioner/network/shape/devices /etc/solo-provisioner/network/shape/classes",
							"Re-run block node uninstall; tc-ingress teardown is idempotent",
						})))
			}
			logx.As().Info().Msg("tc-ingress shape config removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SkippedReport(stp,
				automa.WithDetail("tc-ingress teardown rollback is a no-op; re-enable via block node install"))
		})
}
