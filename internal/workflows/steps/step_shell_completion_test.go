// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shellCompletionHost struct {
	written bool
	removed bool
}

// stubShellCompletionHost scripts the host and records what the step did.
func stubShellCompletionHost(t *testing.T, needsWrite bool, writeErr error) *shellCompletionHost {
	t.Helper()

	origNeeds, origWrite, origRemove := shellCompletionNeedsWrite, reconfigureShellCompletion, removeShellCompletion
	t.Cleanup(func() {
		shellCompletionNeedsWrite = origNeeds
		reconfigureShellCompletion = origWrite
		removeShellCompletion = origRemove
	})

	host := &shellCompletionHost{}
	shellCompletionNeedsWrite = func() bool { return needsWrite }
	reconfigureShellCompletion = func() error { host.written = true; return writeErr }
	removeShellCompletion = func() error { host.removed = true; return nil }

	return host
}

func buildShellCompletionStep(t *testing.T) automa.Step {
	t.Helper()

	step, err := SetupShellCompletion().Build()
	require.NoError(t, err)

	return step
}

func TestSetupShellCompletion_WritesWhenLoadersAreMissing(t *testing.T) {
	host := stubShellCompletionHost(t, true, nil)

	report := buildShellCompletionStep(t).Execute(context.Background())

	assert.True(t, host.written)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, "true", report.Metadata[ConfiguredByThisStep])
	require.NoError(t, report.Error)
}

// A re-install where the loaders are already present must not rewrite them —
// that is what keeps an operator-customised file safe on every install.
func TestSetupShellCompletion_SkipsWriteWhenLoadersArePresent(t *testing.T) {
	host := stubShellCompletionHost(t, false, nil)

	report := buildShellCompletionStep(t).Execute(context.Background())

	assert.False(t, host.written)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, "true", report.Metadata[AlreadyConfigured])
}

// Completion is a convenience: a write failure reports skipped so it cannot
// abort a cluster install.
func TestSetupShellCompletion_WriteFailureDoesNotFailTheStep(t *testing.T) {
	host := stubShellCompletionHost(t, true, errorx.ExternalError.New("read-only file system"))

	report := buildShellCompletionStep(t).Execute(context.Background())

	assert.True(t, host.written)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	require.NoError(t, report.Error)
}

// Rollback removes only what this step wrote, so a --rollback-on-error run does
// not leave loaders behind on a host it just tore down.
func TestSetupShellCompletion_RollbackRemovesWhatItWrote(t *testing.T) {
	host := stubShellCompletionHost(t, true, nil)

	step := buildShellCompletionStep(t)
	require.Equal(t, automa.StatusSuccess, step.Execute(context.Background()).Status)

	report := step.Rollback(context.Background())

	assert.True(t, host.removed)
	assert.Equal(t, automa.StatusSuccess, report.Status)
}

func TestSetupShellCompletion_RollbackSkipsWhenItWroteNothing(t *testing.T) {
	host := stubShellCompletionHost(t, false, nil)

	step := buildShellCompletionStep(t)
	require.Equal(t, automa.StatusSuccess, step.Execute(context.Background()).Status)

	report := step.Rollback(context.Background())

	assert.False(t, host.removed, "must not remove loaders it did not write")
	assert.Equal(t, automa.StatusSkipped, report.Status)
}
