package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

func DaemonReload() automa.Builder {
	return bashSteps.DaemonReload()
}

// SetupCrio2 sets up CRI-O container runtime
// it is going to be obsolete soon
func SetupCrio2() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-crio").Steps(
		bashSteps.DownloadCrio(),
		bashSteps.InstallCrio(),
		bashSteps.DownloadDasel(),
		bashSteps.InstallDasel(),
		bashSteps.ConfigureSandboxCrio(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up CRI-O")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup CRI-O")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "CRI-O setup successfully")
		})
}

// EnableAndStartCrio2 EnableAndStartCrio enables and starts CRI-O service
// it is going to be obsolete soon
func EnableAndStartCrio2() automa.Builder {
	return bashSteps.EnableAndStartCrio().
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Enabling and starting CRI-O")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to enable and start CRI-O")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "CRI-O enabled and started successfully")
		})
}
