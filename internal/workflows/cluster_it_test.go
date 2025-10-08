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

	report := wf.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)

	steps.PrintWorkflowReport(report)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
