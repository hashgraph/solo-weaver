// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package workflows

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins the fix for #1018: the daemon reads daemon.yaml only at startup, so
// install's daemon-config phase must restart it (and probe the result) or a
// daemon left running by uninstall keeps its stale config.
func TestBlockNodeDaemonConfigWorkflow_PairsWriteWithRestartAndProbe(t *testing.T) {
	wb := BlockNodeDaemonConfigWorkflow("hedera-block-node", daemon.StatuszConfig{})

	stp, err := wb.Build()
	require.NoError(t, err)
	wf, ok := stp.(automa.Workflow)
	require.True(t, ok, "expected a Workflow, got %T", stp)

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	assert.Equal(t, []string{
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.CheckDaemonComponentPrerequisitesStepId,
	}, ids)
}
