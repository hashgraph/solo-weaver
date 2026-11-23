// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"

	"github.com/hashgraph/solo-weaver/internal/sysctl"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

const (
	ConfigureSysctlForKubernetesStepId = "configure-sysctl-for-kubernetes"
	SysCtlBackupFilename               = "sysctl.conf"
	KeyBackupFile                      = "backup_file"
	KeyReloadedFiles                   = "reloaded_files"
	KeyCopiedFiles                     = "copied_files"
	KeyCreatedFiles                    = "created_files"
	KeyRemovedFiles                    = "removed_files"
	KeyWarnings                        = "warnings"
)

// lstatPath, sysctlConfigTargets, copySysctlConfiguration and deleteSysctlFiles
// are package vars so tests can stub /etc/sysctl.d.
var (
	lstatPath               = os.Lstat
	sysctlConfigTargets     = sysctl.ConfigurationTargets
	copySysctlConfiguration = sysctl.CopyConfiguration
	deleteSysctlFiles       = sysctl.DeleteFiles
)

// absentPaths returns the paths with nothing at them, not even a directory or a
// dangling symlink, so rollback never removes an entry that was already there.
func absentPaths(paths []string) ([]string, error) {
	var absent []string
	for _, p := range paths {
		_, err := lstatPath(p)
		if os.IsNotExist(err) {
			absent = append(absent, p)
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	return absent, nil
}

func ConfigureSysctlForKubernetes() *automa.WorkflowBuilder {
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
			targets, err := sysctlConfigTargets()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.InternalError.Wrap(err, "failed to list the embedded sysctl templates"),
					reasons.Internal)))
			}

			// Recorded from the targets, not from what the copy reports: a file that
			// fails midway through its write is not reported but is still on disk.
			created, err := absentPaths(targets)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to check the sysctl configuration files"),
					reasons.PermissionDenied,
					"Check that /etc/sysctl.d is readable: 'ls -l /etc/sysctl.d'")))
			}
			stp.State().Local().Set(SysctlFilesCreatedByThisStep, created)
			copied, err := copySysctlConfiguration()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					automa.StepExecutionError.Wrap(err, "failed to copy sysctl configuration files"),
					reasons.PermissionDenied,
					"Check that /etc/sysctl.d is writable and the disk is not full: 'ls -ld /etc/sysctl.d' and 'df -h /etc'")))
			}

			meta := map[string]string{
				KeyCopiedFiles:  strings.Join(copied, ", "),
				KeyCreatedFiles: strings.Join(created, ", "),
			}

			if len(created) == 0 {
				return automa.SkippedReport(stp,
					automa.WithDetail("Sysctl configuration files are already present"), automa.WithMetadata(meta))
			}
			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			created := automa.StringSliceFromState(stp.State().Local(), SysctlFilesCreatedByThisStep)
			if len(created) == 0 {
				return automa.SkippedReport(stp,
					automa.WithDetail("No sysctl configuration files were created by this step, skipping rollback"))
			}

			removed, err := deleteSysctlFiles(created)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					automa.StepExecutionError.Wrap(err, "failed to remove sysctl configuration files"),
					reasons.PermissionDenied,
					fmt.Sprintf("Remove what is left of these files by hand: %s", strings.Join(created, ", ")))))
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
			backupFile, err := sysctl.BackupSettings(path.Join(models.Paths().BackupDir, SysCtlBackupFilename))
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

			stp.State().Local().Set(KeyBackupFile, backupFile)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var backupFile string
			if v, ok := stp.State().Local().String(KeyBackupFile); ok {
				backupFile = v
			}

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
