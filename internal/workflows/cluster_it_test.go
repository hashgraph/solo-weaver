// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package workflows

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/stretchr/testify/require"
)

func Test_NewSetupClusterWorkflow_Integration(t *testing.T) {
	testutil.Reset(t)

	err := testutil.InstallCrioRegistriesConf()
	require.NoError(t, err)

	wf, err := InstallClusterWorkflow(core.NodeTypeBlock, core.ProfileLocal).
		WithExecutionMode(automa.StopOnError).
		Build()
	require.NoError(t, err)

	report := wf.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)

	steps.PrintWorkflowReport(report, "")
	require.Equal(t, automa.StatusSuccess, report.Status)
}
