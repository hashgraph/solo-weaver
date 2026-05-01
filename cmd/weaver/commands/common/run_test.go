// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// TestDeepestFailureError_NilReport verifies the nil guard.
func TestDeepestFailureError_NilReport(t *testing.T) {
	require.NoError(t, deepestFailureError(nil))
}

// TestDeepestFailureError_NoFailures returns nil when every step succeeded.
func TestDeepestFailureError_NoFailures(t *testing.T) {
	r := automa.StepSuccessReport("root", automa.WithStepReports(
		automa.StepSuccessReport("child-a"),
		automa.StepSuccessReport("child-b"),
	))
	require.NoError(t, deepestFailureError(r))
}

// TestDeepestFailureError_TopLevelFailure returns the immediate step's error
// when failures are at the top level (no sub-workflow nesting).
func TestDeepestFailureError_TopLevelFailure(t *testing.T) {
	leafErr := errorx.IllegalState.New("disk full").
		WithProperty(doctor.ErrPropertyResolution, "free up space")

	r := automa.StepFailureReport("root", automa.WithStepReports(
		automa.StepFailureReport("child-a", automa.WithError(leafErr)),
	))

	got := deepestFailureError(r)
	require.ErrorIs(t, got, leafErr)

	// Resolution metadata is preserved on the returned error.
	res, ok := errorx.ExtractProperty(got, doctor.ErrPropertyResolution)
	require.True(t, ok, "expected resolution property on returned error")
	require.Equal(t, "free up space", res)
}

// TestDeepestFailureError_NestedFailure is the regression guard for the bug
// we fixed: when a sub-workflow step fails, automa sets the parent's step
// report Error to a fresh "workflow X completed with N failures" wrapper that
// drops the leaf's errorx properties. deepestFailureError MUST descend past
// that wrapper and return the leaf error so doctor.CheckErr in main.go can
// still extract ErrPropertyResolution and render the resolution panel.
func TestDeepestFailureError_NestedFailure(t *testing.T) {
	leafErr := errorx.IllegalState.New("requires superuser privilege").
		WithProperty(doctor.ErrPropertyResolution, "Run with sudo")

	wrapperErr := errorx.IllegalState.New(
		`workflow "preflight" completed with 1 step failures: [check-superuser]`)

	subWorkflow := automa.StepFailureReport("preflight",
		automa.WithError(wrapperErr),
		automa.WithStepReports(
			automa.StepFailureReport("check-superuser", automa.WithError(leafErr)),
		),
	)

	root := automa.StepFailureReport("install",
		automa.WithStepReports(subWorkflow),
	)

	got := deepestFailureError(root)
	require.ErrorIs(t, got, leafErr,
		"expected leaf error, not the workflow-level wrapper")

	// The resolution that the user needs to see is preserved.
	res, ok := errorx.ExtractProperty(got, doctor.ErrPropertyResolution)
	require.True(t, ok)
	require.Equal(t, "Run with sudo", res)
}

// TestDeepestFailureError_FailedStepWithNilError synthesizes a step-failed
// error so callers always get a non-nil error when a step is marked failed
// but didn't attach one.
func TestDeepestFailureError_FailedStepWithNilError(t *testing.T) {
	r := automa.StepFailureReport("root", automa.WithStepReports(
		automa.StepFailureReport("silent-step"),
	))

	got := deepestFailureError(r)
	require.Error(t, got)
	require.Contains(t, got.Error(), `"silent-step"`)
}

// TestDeepestFailureError_PicksFirstFailureNotSecond verifies that when
// multiple top-level steps failed, the first one wins (consistent with the
// old handleWorkflowResult loop ordering).
func TestDeepestFailureError_PicksFirstFailureNotSecond(t *testing.T) {
	first := errorx.IllegalState.New("first failure")
	second := errorx.IllegalState.New("second failure")

	r := automa.StepFailureReport("root", automa.WithStepReports(
		automa.StepSuccessReport("child-a"),
		automa.StepFailureReport("child-b", automa.WithError(first)),
		automa.StepFailureReport("child-c", automa.WithError(second)),
	))

	got := deepestFailureError(r)
	require.ErrorIs(t, got, first)
}

// TestDeepestFailureError_SkipsSkippedAndSuccessSiblings verifies that
// non-failed siblings before a failed step are skipped, not selected.
func TestDeepestFailureError_SkipsSkippedAndSuccessSiblings(t *testing.T) {
	leafErr := errorx.IllegalState.New("the real failure")

	r := automa.StepFailureReport("root", automa.WithStepReports(
		automa.StepSuccessReport("ok-step"),
		automa.StepSkippedReport("skipped-step"),
		automa.StepFailureReport("bad-step", automa.WithError(leafErr)),
	))

	got := deepestFailureError(r)
	require.ErrorIs(t, got, leafErr)
}
