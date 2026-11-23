// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// RecordStateStepID is the id of the RecordState step.
const RecordStateStepID = "record-state"

// RecordState report metadata keys. WorkflowEntry reads them to name the
// files the step wrote, or to mark the entry NotPersisted.
const (
	metaStateFiles       = "state_files"        // comma-separated files whose content changed
	metaStateNotRecorded = "state_not_recorded" // the write's error, for the entry
	metaStateReason      = "state_reason"       // the write's reason code, for the entry
)

// RecordState writes the command's state with write once the workflow's other
// steps have run. It never fails: under --rollback-on-error a failed step
// would roll back what the workflow just installed. A write that fails leaves
// a warning in the summary and marks the entry NotPersisted instead. The
// records are read from the live host and cluster, so the next command that
// records them fills them in.
func RecordState(write func(ctx context.Context) ([]state.ChangedFile, error)) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(RecordStateStepID).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			files, err := write(ctx)
			meta := automa.StringMap{}
			if len(files) > 0 {
				names := make([]string, len(files))
				for i, f := range files {
					names[i] = string(f)
				}
				meta[metaStateFiles] = strings.Join(names, ",")
			}
			switch {
			case err == nil:
			case errorx.IsOfType(err, fsx.FileSyncError):
				logx.As().Warn().Err(err).Msg("Recorded the state, but could not sync it to disk")
				meta[models.ReportMetaWarning] = "The state was recorded but not synced to disk: " + doctor.OperatorMessage(err) + "."
			default:
				logx.As().Warn().Err(err).Msg("Failed to record the state")
				reason, ok := errx.ReasonOf(err)
				if !ok {
					reason = reasons.StateOperationFailed
				}
				meta[metaStateNotRecorded] = doctor.OperatorMessage(err)
				meta[metaStateReason] = string(reason)
				meta[models.ReportMetaWarning] = "The state was not recorded: " + doctor.OperatorMessage(err) +
					". The next run of this command records it."
			}
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Recording state")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to record state")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			if rpt.Metadata[metaStateNotRecorded] != "" {
				notify.As().StepCompletion(ctx, stp, rpt, "State not recorded (best-effort)")
				return
			}
			notify.As().StepCompletion(ctx, stp, rpt, "State recorded")
		})
}

// recordStateMeta returns the RecordState step's value for key in report's
// workflow, or "" when the step did not run or set none.
func recordStateMeta(report *automa.Report, key string) string {
	if report == nil {
		return ""
	}
	if report.Id == RecordStateStepID {
		return report.Metadata[key]
	}
	for _, sr := range report.StepReports {
		if v := recordStateMeta(sr, key); v != "" {
			return v
		}
	}
	return ""
}

// applyRecordedState names in entry the files the RecordState step wrote, or
// marks entry NotPersisted when it could not write them.
func applyRecordedState(entry *state.ActionHistory, report *automa.Report) {
	if files := recordStateMeta(report, metaStateFiles); files != "" {
		for _, f := range strings.Split(files, ",") {
			entry.Components = append(entry.Components, state.ChangedFile(f))
		}
	}
	if msg := recordStateMeta(report, metaStateNotRecorded); msg != "" {
		entry.NotPersisted = true
		if entry.Error == "" {
			entry.Error = msg
			entry.Reason = errx.Reason(recordStateMeta(report, metaStateReason))
		}
	}
}
