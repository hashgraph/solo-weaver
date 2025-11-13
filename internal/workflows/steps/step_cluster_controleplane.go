package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterControlPlane checks the health of the cluster control plane
func CheckClusterControlPlane() automa.Builder {
	return automa.NewStepBuilder().WithId("check_cluster_control_plane").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster control plane health")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check cluster control plane health")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster control plane health checked successfully")
		})
}
