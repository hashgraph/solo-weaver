package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterNetworking checks if the cluster networking is working
func CheckClusterNetworking() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("check_cluster_networking").Steps(
		CheckClusterDNS(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster networking")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check cluster networking")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster networking checked successfully")
		})
}
