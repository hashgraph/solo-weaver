//go:build integration

package workflows

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/workflows/steps"
)

func Test_NewSetupClusterWorkflow_Integration(t *testing.T) {
	wf, err := NewSetupClusterWorkflow("worker").Build()
	if err != nil {
		t.Fatalf("failed to build workflow: %v", err)
	}

	report, err := wf.Execute(context.Background())
	require.NoError(t, err)

	steps.PrintWorkflowReport(report)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
