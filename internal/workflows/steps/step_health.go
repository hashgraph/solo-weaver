package steps

import (
	"context"
	"os/exec"
	"path/filepath"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

func CheckClusterHealth() automa.Builder {
	return automa.NewStepBuilder().WithId("check-cluster-health").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			script := findScript(filepath.Join("test", "scripts", "health.sh"))
			cmd := exec.CommandContext(ctx, "bash", script)
			outBytes, err := cmd.CombinedOutput()
			out := string(outBytes)

			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err), automa.WithMetadata(map[string]string{
					"output": out,
				}))
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"output": out,
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster health")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Cluster health check failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster is healthy")
		})
}
