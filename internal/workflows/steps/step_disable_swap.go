package steps

import (
	"context"

	"golang.hedera.com/solo-weaver/internal/workflows/notify"

	"github.com/automa-saga/automa"
	osx "golang.hedera.com/solo-weaver/pkg/os"
)

const (
	DisableSwapStepId = "disable-swap"
)

// DisableSwap disables swap on the system
// On execute, it runs the swapoff and ensures fstab is updated to prevent swap from being re-enabled on reboot
// On rollback, it runs the swapon and ensures fstab is updated to re-enable swap on reboot
func DisableSwap() automa.Builder {
	return automa.NewStepBuilder().WithId(DisableSwapStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := osx.DisableSwap()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(automa.StepExecutionError.Wrap(err, "failed to disable swap")))
			}
			return automa.SuccessReport(stp)

		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := osx.EnableSwap()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(automa.StepExecutionError.Wrap(err, "failed to enable swap on rollback")))
			}

			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Disabling swap")
			return ctx, nil

		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to disable swap")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Swap disabled successfully")
		})
}
