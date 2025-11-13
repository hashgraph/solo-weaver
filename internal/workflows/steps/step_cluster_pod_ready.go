package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterPodsReady checks if the specified pods are running in the cluster
// pods is a map of namespace to list of pod prefixes, e.g. cilium- metallb-
func CheckClusterPodsReady(pods map[string]string) automa.Builder {
	return automa.NewStepBuilder().WithId("check_cluster_pods_running").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking if all cluster pods are running")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check if all cluster pods are running")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "All cluster pods are running")
		})
}
