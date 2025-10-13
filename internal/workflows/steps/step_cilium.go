package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

func SetupCiliumCNI() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-cilium").Steps(
		bashSteps.DownloadCiliumCli(),
		bashSteps.InstallCiliumCli(),
		bashSteps.ConfigureCiliumCNI(),
		bashSteps.InstallCiliumCNI(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Cilium CNI")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Cilium CNI")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium CNI setup successfully")
		})
}

func EnableAndStartCiliumCNI() automa.Builder {
	return bashSteps.EnableAndStartCiliumCNI().
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Enabling and starting Cilium CNI")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to enable and start Cilium CNI")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium CNI enabled and started successfully")
		})
}
