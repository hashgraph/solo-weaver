package steps

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/sysctl"
	"golang.hedera.com/solo-weaver/internal/workflows/notify"
)

const (
	ConfigureSysctlForKubernetesStepId = "configure-sysctl-for-kubernetes"
	SysCtlBackupFilename               = "sysctl.conf"
	KeyBackupFile                      = "backup_file"
	KeyReloadedFiles                   = "reloaded_files"
	KeyCopiedFiles                     = "copied_files"
	KeyRemovedFiles                    = "removed_files"
	KeyWarnings                        = "warnings"
)

func ConfigureSysctlForKubernetes() automa.Builder {
	return automa.NewWorkflowBuilder().WithId(ConfigureSysctlForKubernetesStepId).
		Steps(
			copySysctlConfigurationFiles(),
			reloadSysctlSettings(),
		).
		// rollback needs to run in the same as execution order
		// Therefore, we are defining custom rollback for this workflow
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			wf, ok := stp.(automa.Workflow)
			if !ok {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.New("step is not a workflow, cannot rollback")))
			}

			// rollback in the same order as execute as we need to remove files and then reload sysctl
			for i, s := range wf.Steps() {
				report := s.Rollback(ctx)
				if report.Error != nil {
					return automa.FailureReport(stp,
						automa.WithError(
							automa.StepExecutionError.Wrap(report.Error,
								"failed to rollback step %d (%s) in workflow %s", i, s.Id(), stp.Id())))
				}
			}
			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring sysctl for Kubernetes")
			return ctx, nil
		}).
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
			copied, err := sysctl.CopyConfiguration()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to copy sysctl configuration files")))
			}

			meta := map[string]string{
				KeyCopiedFiles: strings.Join(copied, ", "),
			}

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			removed, err := sysctl.DeleteConfiguration()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to remove sysctl configuration files")))
			}

			meta := map[string]string{
				KeyRemovedFiles: strings.Join(removed, ", "),
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

// reloadSysctlSettings reloads sysctl settings to apply any changes made to the configuration files.
// It also creates a backup of the current sysctl settings before reloading.
// In case of failure, it can rollback to the previous settings using the backup file.
func reloadSysctlSettings() automa.Builder {
	return automa.NewStepBuilder().WithId("restart-sysctl-service").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// we need to backup the settings we are going to modify because a full restore is not possible
			// because of permission issues. Also from security and stability perspective, we should never modify settings
			// that are not modified by our process.
			// Therefore, we only backup the settings that are going to be modified by the configuration files. This
			// will be the way to rollback to previous state in case of failure.
			backupFile, err := sysctl.BackupSettings(path.Join(core.Paths().BackupDir, SysCtlBackupFilename))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to backup current sysctl settings")))
			}

			configFiles, err := sysctl.LoadAllConfiguration()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to reload sysctl settings")))
			}

			desiredSettings, err := sysctl.DesiredCandidateSettings()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to get desired candidate sysctl settings")))
			}

			currentSettings, err := sysctl.CurrentCandidateSettings()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to get current candidate sysctl settings")))
			}

			// check that all desired settings are applied
			var warnings []string
			for k, v := range desiredSettings {
				if currentSettings[k] != v {
					warnings = append(warnings,
						fmt.Sprintf("sysctl setting %s is %s but expected %s", k, currentSettings[k], v))
				}
			}

			meta := map[string]string{
				KeyBackupFile:    backupFile,
				KeyReloadedFiles: strings.Join(configFiles, ", "),
				KeyWarnings:      strings.Join(warnings, ","),
			}

			stp.State().Set(KeyBackupFile, backupFile)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			backupFile := stp.State().String(KeyBackupFile)
			if backupFile == "" {
				return automa.SkippedReport(stp, automa.WithDetail("no backup file found in step state, cannot rollback"))
			}

			// check that backup file exists
			if _, err := os.Stat(backupFile); os.IsNotExist(err) {
				return automa.FailureReport(stp, automa.WithError(
					automa.StepExecutionError.
						New("backup file %s does not exist, cannot rollback", backupFile)))
			}

			// load settings from backup file
			err := sysctl.RestoreSettings(backupFile)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to restore sysctl settings from backup file %s", backupFile)))
			}

			meta := map[string]string{
				KeyBackupFile: backupFile,
			}

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Sysctl service restarted")
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to restart sysctl service")
		})
}
