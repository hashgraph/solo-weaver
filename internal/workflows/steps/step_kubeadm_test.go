// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
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

func TestResetCluster_FailsWhenKubeadmVerificationFails(t *testing.T) {
	// A kubeadm that fails its checksum must not run, and the uninstall must not
	// report a reset that never happened.
	stubVerifyExecutables(t, func(string) error {
		return software.NewChecksumError("/opt/solo/weaver/sandbox/bin/kubeadm", "sha512", "expected", "actual")
	})

	step, err := ResetCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	assert.True(t, errorx.IsOfType(report.Error, software.ChecksumError), "the checksum failure must stay visible: %v", report.Error)
	assert.Contains(t, report.Error.Error(), "Kubernetes reset was not run")

	reason, ok := errx.ReasonOf(report.Error)
	require.True(t, ok)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	hints, ok := errx.Hints(report.Error)
	require.True(t, ok)
	assert.NotEmpty(t, hints)
}

func TestResetCluster_FailsWhenKubeadmCannotBeRead(t *testing.T) {
	// Only a missing kubeadm may skip the reset; one that exists but cannot be
	// read is not proof it is gone.
	stubVerifyExecutables(t, func(string) error {
		return software.NewFileSystemError(assert.AnError)
	})

	step, err := ResetCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
}

func TestResetCluster_SkipsResetWhenKubeadmIsAbsent(t *testing.T) {
	// An already-removed kubeadm leaves nothing to reset with; teardown goes on.
	stubVerifyExecutables(t, func(string) error {
		return software.NewFileNotFoundError("/opt/solo/weaver/sandbox/bin/kubeadm")
	})

	step, err := ResetCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
}
