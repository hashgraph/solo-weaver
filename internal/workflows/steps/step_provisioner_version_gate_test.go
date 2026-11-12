// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countPersists stubs persistProvisionerVersion and counts its calls.
func countPersists(t *testing.T) *int {
	t.Helper()
	orig := persistProvisionerVersion
	t.Cleanup(func() { persistProvisionerVersion = orig })
	calls := 0
	persistProvisionerVersion = func() (bool, error) { calls++; return true, nil }
	return &calls
}

// TestRecordProvisionerVersion_OnlyAfterThisRunsInit: the version is recorded
// only for a cluster this run's kubeadm init created. On an existing cluster
// the startup migrations own it, and recording it here would drop one they
// deferred (#1315). --continue-on-error runs the record step after a failed
// init too, so the gate cannot be "init did not skip".
func TestRecordProvisionerVersion_OnlyAfterThisRunsInit(t *testing.T) {
	fresh := func(copyErr error) func(t *testing.T) {
		return func(t *testing.T) {
			stubAPIServer(t, errors.New("connection refused"))
			noControlPlane(t)
			stubKubeadm(t, copyErr)
		}
	}
	tests := []struct {
		name         string
		arrange      func(t *testing.T)
		mode         automa.TypeMode
		wantRecorded bool
	}{
		{"existing cluster answers, init skipped", func(t *testing.T) {
			stubAPIServer(t, nil)
			stubKubeadm(t, nil)
			origRestore := restoreMissingKubeconfigs
			restoreMissingKubeconfigs = func() error { return nil }
			t.Cleanup(func() { restoreMissingKubeconfigs = origRestore })
		}, automa.ContinueOnError, false},
		{"existing control plane down, init refuses", func(t *testing.T) {
			stubAPIServer(t, errors.New("connection refused"))
			leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
			stubKubeadm(t, nil)
		}, automa.ContinueOnError, false},
		{"fresh host, init succeeds", fresh(nil), automa.StopOnError, true},
		{"init succeeds, kubeconfig copy fails, --continue-on-error", fresh(errors.New("disk full")), automa.ContinueOnError, true},
		// The default mode stops at the failed copy, before the record step.
		{"init succeeds, kubeconfig copy fails, --stop-on-error", fresh(errors.New("disk full")), automa.StopOnError, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubVerifyExecutables(t, func(string) error { return nil })
			tc.arrange(t)
			persists := countPersists(t)
			wf, err := automa.NewWorkflowBuilder().WithId("gate").
				WithExecutionMode(tc.mode).
				Steps(InitializeCluster(), RecordProvisionerVersion()).Build()
			require.NoError(t, err)

			wf.Execute(context.Background())

			assert.Equal(t, tc.wantRecorded, *persists == 1, "persist calls: %d", *persists)
		})
	}
}

func TestRecordProvisionerVersion_SkipsWithoutThisRunsInit(t *testing.T) {
	persists := countPersists(t)
	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	assert.Equal(t, automa.StatusSkipped, report.Status)
	assert.Zero(t, *persists)
}
