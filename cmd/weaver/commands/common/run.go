package common

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/doctor"
	"golang.hedera.com/solo-weaver/internal/workflows/steps"
)

// RunWorkflow executes a workflow and handles error
func RunWorkflow(ctx context.Context, b automa.Builder) {
	wb, err := b.Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report := wb.Execute(ctx)
	if report.Error != nil {
		doctor.CheckReportErr(ctx, report)
	}

	// For each step that failed, run the doctor to diagnose the error
	if len(report.StepReports) > 0 {
		for _, stepReport := range report.StepReports {
			if stepReport.Status == automa.StatusFailed {
				doctor.CheckReportErr(ctx, stepReport)
			}
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	reportPath := path.Join(core.Paths().LogsDir, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)
	logx.As().Info().Str("report_path", reportPath).Msg("Workflow report is saved")
}
