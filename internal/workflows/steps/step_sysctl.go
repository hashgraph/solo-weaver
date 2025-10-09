package steps

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/automa-saga/automa"
	sysctl "github.com/lorenzosaino/go-sysctl"
	"golang.hedera.com/solo-provisioner/internal/templates"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

const ConfigureSysctlForKubernetesStepId = "configure-sysctl-for-kubernetes"

func ConfigureSysctlForKubernetes() automa.Builder {
	return automa.NewWorkflowBuilder().WithId(ConfigureSysctlForKubernetesStepId).
		Steps(
			copySysctlConfigurationFiles(),
			restartSysctlService(),
		).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to configure sysctl for Kubernetes")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Sysctl configured for Kubernetes")
		})
}

func copySysctlConfigurationFiles() automa.Builder {
	return automa.NewStepBuilder().WithId("copy-sysctl-files").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			copied, err := templates.CopySysctlConfigurationFiles()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to copy sysctl configuration files")))
			}

			meta := map[string]string{
				"copied_files": strings.Join(copied, ", "),
			}

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			removed, err := templates.RemoveSysctlConfigurationFiles()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to remove sysctl configuration files")))
			}

			meta := map[string]string{
				"removed_files": strings.Join(removed, ", "),
			}

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Sysctl configuration files copied")
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to copy sysctl configuration files")
		})
}

func restartSysctlService() automa.Builder {
	return automa.NewStepBuilder().WithId("restart-sysctl-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Reload sysctl settings
			configFiles, err := templates.ReadDir(templates.SysctlConfigSourceDir)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to read sysctl configuration directory")))
			}

			for i := range configFiles {
				configFiles[i] = path.Join(templates.SysctlConfigDestDir, configFiles[i])
			}

			sort.Strings(configFiles)

			err = sysctl.LoadConfigAndApply(configFiles...)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to reload sysctl settings")))
			}

			return automa.SuccessReport(stp)
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Sysctl service restarted")
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to restart sysctl service")
		})
}
