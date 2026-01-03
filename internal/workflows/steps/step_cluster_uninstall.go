// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	osutil "github.com/hashgraph/solo-weaver/pkg/os"
)

// RemoveSystemdServiceFiles removes systemd service files created during cluster setup
func RemoveSystemdServiceFiles() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("remove-systemd-service-files").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing systemd service files")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove systemd service files")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Systemd service files removed successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			fsManager, err := fsx.NewManager()
			if err != nil {
				logx.As().Warn().Err(err).Msg("Failed to create filesystem manager, continuing with teardown")
				return automa.SuccessReport(stp)
			}

			// Remove systemd service files
			filesToRemove := []string{
				"/usr/lib/systemd/system/crio.service",
				"/usr/lib/systemd/system/kubelet.service.d",
				"/usr/lib/systemd/system/kubelet.service",
			}

			for _, file := range filesToRemove {
				if err := fsManager.RemoveAll(file); err != nil {
					logx.As().Warn().Err(err).Msgf("Failed to remove %s, continuing with teardown", file)
				}
			}

			// Reload systemd daemon
			if err := osutil.DaemonReload(ctx); err != nil {
				logx.As().Warn().Err(err).Msg("Failed to reload systemd daemon, continuing with teardown")
			}

			return automa.SuccessReport(stp)
		})
}

// RemoveConfigDirectories removes configuration directories created during cluster setup
func RemoveConfigDirectories() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("remove-config-directories").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing configuration directories")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove configuration directories")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Configuration directories removed successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			fsManager, err := fsx.NewManager()
			if err != nil {
				logx.As().Warn().Err(err).Msg("Failed to create filesystem manager, continuing with teardown")
				return automa.SuccessReport(stp)
			}

			directoriesToRemove := []string{
				"/etc/containers",
				"/etc/crio",
				"/root/.kube",
				"/home/weaver/.kube",
			}

			for _, dir := range directoriesToRemove {
				if err := fsManager.RemoveAll(dir); err != nil {
					logx.As().Warn().Err(err).Msgf("Failed to remove %s, continuing with teardown", dir)
				}
			}

			return automa.SuccessReport(stp)
		})
}

// CleanupWeaverFiles removes weaver installation files while preserving downloads, bin, and logs folders
func CleanupWeaverFiles() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("cleanup-weaver-files").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Cleaning up weaver files")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to cleanup weaver files")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Weaver files cleaned up successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			fsManager, err := fsx.NewManager()
			if err != nil {
				logx.As().Warn().Err(err).Msg("Failed to create filesystem manager, continuing with teardown")
				return automa.SuccessReport(stp)
			}

			weaverHome := "/opt/solo/weaver"

			// Read the weaver home directory
			entries, err := os.ReadDir(weaverHome)
			if err != nil {
				// Directory doesn't exist, nothing to clean
				logx.As().Debug().Err(err).Msg("Weaver home directory doesn't exist, nothing to clean")
				return automa.SuccessReport(stp)
			}

			// Remove each top-level directory/file except downloads
			for _, entry := range entries {
				entryPath := filepath.Join(weaverHome, entry.Name())

				// Skip the downloads, bin, and logs folders
				if entryPath == core.Paths().DownloadsDir ||
					entryPath == core.Paths().BinDir ||
					entryPath == core.Paths().LogsDir {
					continue
				}

				// Remove all other directories/files
				if err := fsManager.RemoveAll(entryPath); err != nil {
					logx.As().Warn().Err(err).Msgf("Failed to remove %s, continuing with teardown", entryPath)
				}
			}

			return automa.SuccessReport(stp)
		})
}
