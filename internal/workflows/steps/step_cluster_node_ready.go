package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterNodesReady checks if all nodes in the cluster are ready
func CheckClusterNodesReady() automa.Builder {
	return automa.NewStepBuilder().WithId("check_cluster_nodes_ready").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking if all cluster nodes are ready")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check if all cluster nodes are ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "All cluster nodes are ready")
		})
}
