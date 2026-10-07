// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
)

func newReport() *automa.Report {
	now := time.Now()
	return &automa.Report{StartTime: now, EndTime: now.Add(time.Second)}
}

func TestRenderSummaryTable_IncludesDaemonLogWhenNonEmpty(t *testing.T) {
	out := RenderSummaryTable(newReport(), time.Second, "/logs/report.yaml", "/logs/solo-provisioner.log", "/logs/solo-provisioner-daemon.log")
	assert.Contains(t, out, "Daemon log:")
	assert.Contains(t, out, "/logs/solo-provisioner-daemon.log")
}

func TestRenderSummaryTable_OmitsDaemonLogWhenEmpty(t *testing.T) {
	out := RenderSummaryTable(newReport(), time.Second, "/logs/report.yaml", "/logs/solo-provisioner.log", "")
	assert.False(t, strings.Contains(out, "Daemon log:"), "daemon log line should be absent when daemonLogPath is empty")
}

func TestRenderSummaryTable_OmitsWarningsBlockWhenNoneRecorded(t *testing.T) {
	// This table is shared by every command, so a run without warnings must
	// render byte-identical to a build without the warnings block at all.
	report := newReport()
	report.StepReports = []*automa.Report{
		{Id: "step-a", Metadata: automa.StringMap{"storage.archive.fstype": "xfs"}},
	}

	out := RenderSummaryTable(report, time.Second, "/logs/report.yaml", "/logs/solo-provisioner.log", "")
	expected := RenderSummaryTable(newReport(), time.Second, "/logs/report.yaml", "/logs/solo-provisioner.log", "")

	assert.Equal(t, expected, out)
	assert.NotContains(t, out, "Warnings:")
}

func TestRenderSummaryTable_RendersWarningsLast(t *testing.T) {
	report := newReport()
	report.StepReports = []*automa.Report{
		{Id: "check-storage-media", Metadata: automa.StringMap{
			models.ReportMetaWarning: "/mnt/s3 (archive) is on fuse.s3fs, not local block storage",
		}},
	}

	out := RenderSummaryTable(report, time.Second, "/logs/report.yaml", "", "")
	assert.Contains(t, out, "Warnings:")
	assert.Contains(t, out, "/mnt/s3 (archive) is on fuse.s3fs")
	assert.Greater(t, strings.Index(out, "Warnings:"), strings.Index(out, "Report:"))
}

func TestCollectWarnings_WalksNestedReportsAndDeduplicates(t *testing.T) {
	warn := "/mnt/s3 (archive) is on fuse.s3fs, not local block storage"
	report := &automa.Report{
		StepReports: []*automa.Report{
			{Id: "phase", StepReports: []*automa.Report{
				{Id: "nested", Metadata: automa.StringMap{models.ReportMetaWarning: warn + "\nsecond warning"}},
			}},
			// The same step reported again (e.g. a retried phase) must not
			// duplicate its warning in the summary.
			{Id: "nested", Metadata: automa.StringMap{models.ReportMetaWarning: warn}},
			{Id: "quiet", Metadata: automa.StringMap{"storage.live.fstype": "ext4"}},
		},
	}

	assert.Equal(t, []string{warn, "second warning"}, CollectWarnings(report))
}

func TestCollectWarnings_NilAndEmpty(t *testing.T) {
	assert.Empty(t, CollectWarnings(nil))
	assert.Empty(t, CollectWarnings(&automa.Report{}))
}

func TestRenderSummaryTable_RendersStorageMediaBeforeWarnings(t *testing.T) {
	report := newReport()
	report.StepReports = []*automa.Report{
		{Id: "check-storage-media", Metadata: automa.StringMap{
			models.ReportMetaStorageMedia: "archive: fuse.s3fs on /mnt/s3 (not local block storage)\nlive: xfs on /mnt/data",
			models.ReportMetaWarning:      "/mnt/s3 (archive) is on fuse.s3fs, not local block storage",
		}},
	}

	out := RenderSummaryTable(report, time.Second, "/logs/report.yaml", "", "")
	assert.Contains(t, out, "Storage media:")
	assert.Contains(t, out, "archive: fuse.s3fs on /mnt/s3 (not local block storage)")
	assert.Contains(t, out, "live: xfs on /mnt/data")
	assert.Less(t, strings.Index(out, "Storage media:"), strings.Index(out, "Warnings:"))
}

func TestRenderSummaryTable_OmitsStorageMediaBlockWhenNoneRecorded(t *testing.T) {
	report := newReport()
	report.StepReports = []*automa.Report{
		{Id: "step-a", Metadata: automa.StringMap{"storage.archive.fstype": "xfs"}},
	}

	assert.NotContains(t, RenderSummaryTable(report, time.Second, "/logs/report.yaml", "", ""), "Storage media:")
}

func TestCollectStorageMedia_DeduplicatesRepeatedLines(t *testing.T) {
	// The walker is shared with CollectWarnings: a line recorded by more than
	// one step must reach the operator once, not once per step.
	line := "archive: xfs on /mnt/data"
	report := &automa.Report{
		StepReports: []*automa.Report{
			{Id: "preflight", Metadata: automa.StringMap{models.ReportMetaStorageMedia: line}},
			{Id: "deploy", Metadata: automa.StringMap{models.ReportMetaStorageMedia: line + "\nlive: xfs on /mnt/data"}},
		},
	}

	assert.Equal(t, []string{line, "live: xfs on /mnt/data"}, collectStorageMedia(report))
}
