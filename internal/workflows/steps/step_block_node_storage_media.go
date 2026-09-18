// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/mount"
	"github.com/hashgraph/solo-weaver/internal/mount/mountinfo"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// CheckStorageMediaStepId is the workflow step ID of the storage media check.
const CheckStorageMediaStepId = "check-storage-media"

// storageClassifier reports the mount backing each volume-name -> path entry.
// Injected for testing; production wiring passes mount.ClassifyStoragePaths.
type storageClassifier func(map[string]string) ([]mountinfo.Finding, error)

// BlockNodeStorageMediaStep warns when a block-node storage path is not on
// local block storage. It's warn-only: it never fails or rolls back the workflow.
func BlockNodeStorageMediaStep(storage models.BlockNodeStorage, chartVersion string) automa.Builder {
	paths, err := blocknode.StoragePathsByVolume(storage, chartVersion)
	if err != nil {
		logx.As().Warn().Err(err).
			Msg("Skipping storage media check: storage paths could not be resolved")
	}
	return checkStorageMediaStep(paths, mount.ClassifyStoragePaths)
}

// checkStorageMediaStep is the classifier-injected core of BlockNodeStorageMediaStep.
func checkStorageMediaStep(paths map[string]string, classify storageClassifier) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(CheckStorageMediaStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if classify == nil {
				logx.As().Warn().Msg("Skipping storage media check: no storage classifier is wired")
				return skippedMediaReport(stp, "no storage classifier is wired")
			}
			if len(paths) == 0 {
				logx.As().Info().Msg("Skipping storage media check: no storage paths resolved")
				return skippedMediaReport(stp, "the storage paths could not be resolved")
			}

			findings, err := classify(paths)
			if err != nil {
				logx.As().Warn().Err(err).
					Msg("Skipping storage media check: failed to read the mount table")
				return skippedMediaReport(stp, "the mount table could not be read")
			}
			if len(findings) == 0 {
				logx.As().Info().
					Msg("Skipping storage media check: no storage path resolved to a known mount")
				return skippedMediaReport(stp, "no storage path resolved to a known mount")
			}

			summary := mountinfo.Summarize(findings)
			// The per-volume media lines are what `block node check` shows the
			// operator; the fstype/mountpoint entries stay for the saved report.
			if len(summary.Media) > 0 {
				summary.Metadata[models.ReportMetaStorageMedia] = strings.Join(summary.Media, "\n")
			}
			if summary.Warning != "" {
				summary.Metadata[models.ReportMetaWarning] = summary.Warning
				logx.As().Warn().
					Str("warning", summary.Warning).
					Msg("Block node storage path is not on local block storage")
			}

			return automa.SuccessReport(stp, automa.WithMetadata(summary.Metadata))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking block node storage media")
			return ctx, nil
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node storage media checked")
		})
}

// skippedMediaReport skips the check but still says why on the console: console
// logging is suppressed under the TUI, so silence would read as "all paths fine".
func skippedMediaReport(stp automa.Step, reason string) *automa.Report {
	return automa.SkippedReport(stp, automa.WithMetadata(map[string]string{
		models.ReportMetaStorageMedia: "not determined: " + reason + " (see the log)",
	}))
}
