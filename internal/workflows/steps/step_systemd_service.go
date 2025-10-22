package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/os"
)

// SetupSystemdService enables and starts a systemd service by name
// It also reloads the systemd daemon to apply any changes
// Example: SetupSystemdService("kubelet")
func SetupSystemdService(serviceName string) automa.Builder {
	stepId := fmt.Sprintf("setup-systemd-service-%s", serviceName)

	return automa.NewStepBuilder().WithId(stepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Enabling and starting %s", serviceName))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, fmt.Sprintf("Failed to enable and start %s", serviceName))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf("%s enabled and started successfully", serviceName))
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := os.DaemonReload(ctx)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = os.EnableService(ctx, serviceName)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = os.StartService(ctx, serviceName)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}
