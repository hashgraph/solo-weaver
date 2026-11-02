// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSwap replaces the host swap calls and records which ran. inUse is what
// the probe reports before the step acts.
func stubSwap(t *testing.T, inUse bool) *[]string {
	t.Helper()
	calls := &[]string{}
	origInUse, origDisable, origEnable := swapInUse, disableSwap, enableSwap
	swapInUse = func() (bool, error) { return inUse, nil }
	disableSwap = func() error { *calls = append(*calls, "disable"); return nil }
	enableSwap = func() error { *calls = append(*calls, "enable"); return nil }
	t.Cleanup(func() { swapInUse, disableSwap, enableSwap = origInUse, origDisable, origEnable })
	return calls
}

// Rollback scope: disable-swap re-enables swap only when this run turned it off.

func TestDisableSwap_AlreadyOffIsSkippedAndNotRolledBack(t *testing.T) {
	calls := stubSwap(t, false)

	step, err := DisableSwap().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Empty(t, *calls, "a host that had no swap in use must be left alone")
}

func TestDisableSwap_InUseIsDisabledAndRolledBack(t *testing.T) {
	calls := stubSwap(t, true)

	step, err := DisableSwap().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.Equal(t, []string{"disable", "enable"}, *calls)
}

// A DisableSwap that fails midway has already touched the host (units masked,
// fstab edited), so the failed step's rollback must still re-enable swap.
func TestDisableSwap_MidwayFailureIsRolledBack(t *testing.T) {
	calls := stubSwap(t, true)
	disableSwap = func() error { *calls = append(*calls, "disable"); return assert.AnError }

	step, err := DisableSwap().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.Equal(t, []string{"disable", "enable"}, *calls)
}
