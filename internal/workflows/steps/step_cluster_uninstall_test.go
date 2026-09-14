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

// stubRemoveShellCompletion scripts the loader removal and reports whether it ran.
func stubRemoveShellCompletion(t *testing.T, err error) *bool {
	t.Helper()

	orig := removeShellCompletion
	t.Cleanup(func() { removeShellCompletion = orig })

	called := false
	removeShellCompletion = func() error {
		called = true
		return err
	}

	return &called
}

func runRemoveLoadersStep(t *testing.T) *automa.Report {
	t.Helper()

	step, err := RemoveShellCompletionLoaders().Build()
	require.NoError(t, err)

	return step.Execute(context.Background())
}

func TestRemoveShellCompletionLoaders_RemovesThem(t *testing.T) {
	called := stubRemoveShellCompletion(t, nil)

	report := runRemoveLoadersStep(t)

	assert.True(t, *called)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	require.NoError(t, report.Error)
}

// Teardown is best-effort, so a removal failure must not fail the workflow —
// but it must not be reported as success either, or the run claims work it did
// not do.
func TestRemoveShellCompletionLoaders_FailureIsSkippedNotFailed(t *testing.T) {
	called := stubRemoveShellCompletion(t, errorx.ExternalError.New("read-only file system"))

	report := runRemoveLoadersStep(t)

	assert.True(t, *called)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	require.NoError(t, report.Error, "a skipped teardown step must not carry an error")
}
