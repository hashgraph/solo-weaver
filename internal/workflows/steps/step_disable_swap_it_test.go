//go:build integration

package steps

import (
	"context"
	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_DisableSwapWorkflow_Integration(t *testing.T) {
	wf := disableSwapWorkflow()
	step, err := wf.Build()
	if err != nil {
		t.Fatalf("failed to build disableSwapWorkflow: %v", err)
	}

	report, err := step.Execute(context.Background())
	if err != nil {
		t.Errorf("disableSwapWorkflow execution failed: %v", err)
	}

	PrintWorkflowReport(report)
	require.Equal(t, automa.StatusSuccess, report.Status)
}

func Test_RestoreSwapWorkflow_Integration(t *testing.T) {
	wf := restoreSwapWorkflow()
	step, err := wf.Build()
	if err != nil {
		t.Fatalf("failed to build restoreSwapWorkflow: %v", err)
	}

	report, err := step.Execute(context.Background())
	if err != nil {
		t.Errorf("restoreSwapWorkflow execution failed: %v", err)
	}

	PrintWorkflowReport(report)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
