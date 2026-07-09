// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubVerifyExecutables swaps the package-level checksum verifier for the
// duration of a test and restores it on cleanup. It returns a pointer to the
// slice of artifact names the code under test asked to verify, so a test can
// assert both that verification ran and which binary it targeted.
func stubVerifyExecutables(t *testing.T, fn func(artifactName string) error) *[]string {
	t.Helper()
	orig := verifyExecutables
	calls := &[]string{}
	verifyExecutables = func(artifactName string) error {
		*calls = append(*calls, artifactName)
		return fn(artifactName)
	}
	t.Cleanup(func() { verifyExecutables = orig })
	return calls
}

// VerifyExecutablesStep is the only time-of-use checksum check for the
// systemd-launched binaries (kubelet, cri-o + runtimes, teleport), so these
// two cases pin that a clean binary lets the workflow proceed and a tampered
// one blocks it before the service starts.

func TestVerifyExecutablesStep_PassesWhenVerificationSucceeds(t *testing.T) {
	calls := stubVerifyExecutables(t, func(string) error { return nil })

	step, err := VerifyExecutablesStep("kubelet").Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, []string{"kubelet"}, *calls, "the step must verify the artifact it was built for")
}

func TestVerifyExecutablesStep_FailsWhenVerificationFails(t *testing.T) {
	stubVerifyExecutables(t, func(string) error { return assert.AnError })

	step, err := VerifyExecutablesStep("cri-o").Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
}
