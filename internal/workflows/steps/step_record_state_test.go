// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/require"
)

// runRecordState runs a workflow holding only a RecordState step with write,
// and returns its report and the entry WorkflowEntry builds from it.
func runRecordState(t *testing.T, write func(context.Context) ([]state.ChangedFile, error)) (*automa.Report, state.ActionHistory) {
	t.Helper()
	wf, err := automa.NewWorkflowBuilder().WithId("record").Steps(RecordState(write)).Build()
	require.NoError(t, err)
	report := wf.Execute(context.Background())
	return report, WorkflowEntry(context.Background(), models.Intent{}, nil, report)
}

func TestRecordState_NamesTheFilesItWrote(t *testing.T) {
	report, entry := runRecordState(t, func(context.Context) ([]state.ChangedFile, error) {
		return []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, nil
	})

	require.True(t, report.IsSuccess())
	require.Equal(t, []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, entry.Components)
	require.False(t, entry.NotPersisted)
}

// A write that fails never fails the step; it marks the entry NotPersisted
// with the write's error and leaves a warning.
func TestRecordState_FailedWriteIsAWarning(t *testing.T) {
	report, entry := runRecordState(t, func(context.Context) ([]state.ChangedFile, error) {
		return nil, errors.New("disk full")
	})

	require.True(t, report.IsSuccess())
	require.Equal(t, state.OutcomeSucceeded, entry.Outcome)
	require.True(t, entry.NotPersisted)
	require.Equal(t, "disk full", entry.Error)
	require.Equal(t, reasons.StateOperationFailed, entry.Reason)
	require.Contains(t, report.StepReports[0].Metadata[models.ReportMetaWarning], "The state was not recorded: disk full")
}

// A write that landed but was not synced names its files and warns, but is
// not NotPersisted.
func TestRecordState_UnsyncedWriteNamesItsFiles(t *testing.T) {
	report, entry := runRecordState(t, func(context.Context) ([]state.ChangedFile, error) {
		return []state.ChangedFile{state.ComponentCluster.File()}, fsx.FileSyncError.New("fsync failed")
	})

	require.True(t, report.IsSuccess())
	require.Equal(t, []state.ChangedFile{state.ComponentCluster.File()}, entry.Components)
	require.False(t, entry.NotPersisted)
	require.Contains(t, report.StepReports[0].Metadata[models.ReportMetaWarning], "not synced to disk")
}
