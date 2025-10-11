package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

func SetupKubelet() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubelet").
		Steps(
			bashSteps.DownloadKubelet(),
			bashSteps.InstallKubelet(),
			bashSteps.DownloadKubeletConfig(),
			bashSteps.ConfigureKubelet(),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up kubelet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup kubelet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubelet setup successfully")
		})
}

func EnableAndStartKubelet() automa.Builder {
	return bashSteps.EnableAndStartKubelet().
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Enabling and starting kubelet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to enable and start kubelet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubelet enabled and started successfully")
		})
}
