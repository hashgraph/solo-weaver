package steps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/templates"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

func CheckClusterHealth() automa.Builder {
	return automa.NewStepBuilder().WithId("check-cluster-health").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Define paths
			scriptDir := "/opt/provisioner/bin"
			scriptPath := filepath.Join(scriptDir, "health.sh")
			templateSrc := "files/health/health.sh"

			// Ensure the directory exists
			if err := os.MkdirAll(scriptDir, 0755); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err), automa.WithMetadata(map[string]string{
					"error": "failed to create script directory: " + scriptDir,
				}))
			}

			// Copy the script from templates to the target location
			if err := templates.CopyTemplateFile(templateSrc, scriptPath); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err), automa.WithMetadata(map[string]string{
					"error": "failed to copy health script from templates",
				}))
			}

			// Make the script executable
			if err := os.Chmod(scriptPath, 0755); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err), automa.WithMetadata(map[string]string{
					"error": "failed to make script executable",
				}))
			}

			// Execute the script
			cmd := exec.CommandContext(ctx, "bash", scriptPath)
			outBytes, err := cmd.CombinedOutput()
			out := string(outBytes)

			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err), automa.WithMetadata(map[string]string{
					"output": out,
				}))
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"output":      out,
				"script_path": scriptPath,
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
