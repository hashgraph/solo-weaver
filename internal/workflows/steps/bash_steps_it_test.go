//go:build mock_provisioner

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

func Test_BashScriptBasedClusterSetupWorkflow_Integration(t *testing.T) {
	registry := BashScriptBasedStepRegistry()

	wf, err := registry.Of(SetupClusterStepId).Build()
	require.NoError(t, err)

	report, err := wf.Execute(context.Background())
	require.NoError(t, err)

	PrintWorkflowReport(report)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
