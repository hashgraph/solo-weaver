// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

// persistProvisionerVersion records the running CLI version to the on-disk state
// file. A var so tests can stub it to exercise the step's best-effort swallow
// without a writable state dir.
var persistProvisionerVersion = state.PersistProvisionerVersion

// RecordProvisionerVersion persists the running CLI version to state.yaml at the
// end of cluster install. `kube cluster install` does not flush state via the
// BaseHandler path (unlike `block node install`), so without this a fresh cluster
// has no state.yaml and the next invocation synthesises the 0.0.0 baseline and
// re-runs historical migrations.
func RecordProvisionerVersion() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("record-provisioner-version").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := persistProvisionerVersion(); err != nil {
				logx.As().Warn().Err(err).
					Msg("Failed to record provisioner version after cluster install; " +
						"startup migrations may re-evaluate historical boundaries on the next invocation")
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Recording provisioner version")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to record provisioner version")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Provisioner version recorded")
		})
}
