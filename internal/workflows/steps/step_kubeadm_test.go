// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the time-of-use checksum guards wired into the kubeadm
// steps. They stub the package verifier so the failure paths return before any
// binary is executed, which is exactly the tampering-blocks-execution invariant
// the guards exist to enforce.

func TestInitializeCluster_FailsWhenKubectlVerificationFails(t *testing.T) {
	calls := stubVerifyExecutables(t, func(string) error { return assert.AnError })

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	// kubectl is verified first; failing it must abort before kubeadm runs.
	assert.Equal(t, []string{"kubectl"}, *calls)
}

func TestInitializeCluster_RollbackFailsWhenKubeadmVerificationFails(t *testing.T) {
	calls := stubVerifyExecutables(t, func(string) error { return assert.AnError })

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Rollback(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Equal(t, []string{"kubeadm"}, *calls)
}

func TestResetCluster_SkipsResetWhenKubeadmVerificationFails(t *testing.T) {
	// Teardown is best-effort: a failed checksum must warn and continue (success)
	// rather than fail closed, so a tampered kubeadm never runs but teardown of
	// the rest of the stack is not blocked.
	stubVerifyExecutables(t, func(string) error { return assert.AnError })

	step, err := ResetCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "reset must not fail closed when verification fails")
	assert.Equal(t, automa.StatusSuccess, report.Status)
}
