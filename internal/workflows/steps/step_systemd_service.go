package steps

import (
	"context"
	"fmt"
	"strconv"

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

			// Prepare metadata for reporting
			meta := map[string]string{}

			serviceEnabled, err := os.IsServiceEnabled(ctx, serviceName)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			stp.State().Set(ServiceAlreadyEnabled, serviceEnabled)
			meta[ServiceAlreadyEnabled] = strconv.FormatBool(serviceEnabled)

			if !serviceEnabled {
				err = os.EnableService(ctx, serviceName)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
				}
				stp.State().Set(ServiceEnabledByThisStep, true)
				meta[ServiceEnabledByThisStep] = "true"
			}

			serviceRunning, err := os.IsServiceRunning(ctx, serviceName)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			stp.State().Set(ServiceAlreadyRunning, serviceRunning)
			meta[ServiceAlreadyRunning] = strconv.FormatBool(serviceRunning)

			if !serviceRunning {
				err = os.RestartService(ctx, serviceName)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
				}
				stp.State().Set(ServiceStartedByThisStep, true)
				meta[ServiceStartedByThisStep] = "true"
			}

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			serviceAlreadyEnabled := stp.State().Bool(ServiceAlreadyEnabled)
			serviceAlreadyRunning := stp.State().Bool(ServiceAlreadyRunning)

			if serviceAlreadyEnabled && serviceAlreadyRunning {
				return automa.SkippedReport(stp, automa.WithDetail("service was not modified by this step, skipping rollback"))
			}

			err := os.DaemonReload(ctx)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			serviceStartedByThisStep := stp.State().Bool(ServiceStartedByThisStep)
			if serviceStartedByThisStep {
				err = os.StopService(ctx, serviceName)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
			}

			serviceEnabledByThisStep := stp.State().Bool(ServiceEnabledByThisStep)
			if serviceEnabledByThisStep {
				err = os.DisableService(ctx, serviceName)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
			}

			return automa.SuccessReport(stp)
		})
}
