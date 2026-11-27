// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// WaitClusterReadableStepID is the id of the WaitClusterReadable step.
const WaitClusterReadableStepID = "wait-cluster-readable"

// WaitClusterReadable waits until read reports that the whole cluster can be
// read, trying every interval for up to timeout. read returns nil when it can,
// or why it cannot. Placed before RecordState, it keeps a change from being
// recorded from a cluster that is still settling.
func WaitClusterReadable(read func(ctx context.Context) error, timeout, interval time.Duration) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(WaitClusterReadableStepID).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if read == nil || interval <= 0 || timeout < interval {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalArgument.New("waiting for the cluster needs a read function and 0 < interval <= timeout"),
					reasons.Internal)))
			}

			var last error
			err := wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
				last = read(ctx)
				if last != nil {
					logx.As().Debug().Err(last).Msg("The cluster cannot be fully read yet")
				}
				return last == nil, nil
			})
			if err == nil {
				return automa.StepSuccessReport(stp.Id())
			}

			cause := last
			if cause == nil {
				cause = err // cancelled before the first read
			}
			logx.As().Warn().Err(cause).Dur("timeout", timeout).Msg("The cluster could not be fully read in time")
			return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
				errorx.IllegalState.Wrap(cause, "the cluster could not be fully read within %s", timeout),
				reasons.PreconditionNotMet,
				"Check the API server with 'kubectl get --raw /readyz' and the node with 'kubectl get nodes'.",
				"Run the command again once both answer; it continues from where the cluster is now.")))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Waiting until the cluster can be fully read")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "The cluster could not be fully read")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "The cluster can be fully read")
		})
}
