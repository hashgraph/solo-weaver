package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

const (
	metaLBVersion = "0.15.2"
)

func SetupMetalLB() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-metallb").Steps(
		installMetalLB(metaLBVersion), // still using bash steps
		configureMetalLB(),            // still using bash steps
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up MetalLB")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup MetalLB")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB setup successfully")
		})
}
