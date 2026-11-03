// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"
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

func TestInitializeCluster_FailsWhenKubeadmVerificationFails(t *testing.T) {
	stubAPIServer(t, assert.AnError) // no cluster answers: a fresh host
	noControlPlane(t)
	calls := stubVerifyExecutables(t, func(string) error { return assert.AnError })

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	// kubeadm is verified before it runs; failing it must abort the init.
	assert.Equal(t, []string{"kubeadm"}, *calls)

	// An init that never ran leaves nothing in the rollback scope.
	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Equal(t, []string{"kubeadm"}, *calls, "kubeadm reset must not run after a failed init")
}

func TestInitializeCluster_RollbackFailsWhenKubeadmVerificationFails(t *testing.T) {
	calls := stubVerifyExecutables(t, func(string) error { return assert.AnError })

	step, err := InitializeCluster().Build()
	require.NoError(t, err)
	// A cluster this run initialized is in the rollback scope.
	step.State().Local().Set(InitializedByThisStep, true)

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

// Rollback scope: init-cluster resets a cluster only when this run initialized
// it. On a host that already has a cluster the step skips, and under
// --rollback-on-error its rollback must leave that cluster alone.

// stubKubeconfigCopies counts the full and missing-only kubeconfig copies.
func stubKubeconfigCopies(t *testing.T) (full, missingOnly *int) {
	t.Helper()
	full, missingOnly = new(int), new(int)
	origCopy, origRestore := configureKubeconfig, restoreMissingKubeconfigs
	configureKubeconfig = func() error { *full++; return nil }
	restoreMissingKubeconfigs = func() error { *missingOnly++; return nil }
	t.Cleanup(func() { configureKubeconfig, restoreMissingKubeconfigs = origCopy, origRestore })
	return full, missingOnly
}

func TestInitializeCluster_ExistingClusterIsSkippedAndNotRolledBack(t *testing.T) {
	probed := stubAPIServer(t, nil) // the API server answers: a cluster exists
	full, missingOnly := stubKubeconfigCopies(t)
	calls := stubVerifyExecutables(t, func(string) error {
		t.Fatal("no binary may be verified or run on an existing cluster")
		return nil
	})

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	assert.Equal(t, []string{"/etc/kubernetes/admin.conf"}, *probed)
	assert.Zero(t, *full, "an existing cluster's kubeconfigs are never overwritten")
	assert.Equal(t, 1, *missingOnly, "missing kubeconfigs are restored")

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Empty(t, *calls, "kubeadm reset must not run for a cluster this run did not create")
}

// A failed restore of a missing kubeconfig fails the step: later steps need it.
func TestInitializeCluster_ExistingClusterKubeconfigRestoreFailureFails(t *testing.T) {
	stubAPIServer(t, nil)
	stubKubeconfigCopies(t)
	restoreMissingKubeconfigs = func() error { return assert.AnError }

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	reason, _ := errx.ReasonOf(report.Error)
	assert.Equal(t, reasons.PermissionDenied, reason)
}

// kubeadm init that fails preflight has written nothing, and whatever made it
// fail (a non-empty /var/lib/etcd, a running kubelet) is not this run's to reset.
func TestInitializeCluster_FailedPreflightIsNotReset(t *testing.T) {
	stubAPIServer(t, assert.AnError)
	noControlPlane(t)
	stubVerifyExecutables(t, func(string) error { return nil })
	scripts := stubKubeadm(t, nil)
	runBashScript = func(s []string, _ string) (string, error) {
		*scripts = append(*scripts, s...)
		if hasScript(s, "kubeadm init phase preflight") {
			return "", assert.AnError
		}
		return "", nil
	}

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	reason, _ := errx.ReasonOf(report.Error)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	assert.False(t, hasScript(*scripts, "kubeadm init --upload-certs"), "init must not run after a failed preflight")

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.False(t, hasScript(*scripts, "kubeadm reset"))
}

// stubKubeadm makes every kubeadm command succeed, records the scripts run,
// and makes the kubeconfig copy return copyErr.
func stubKubeadm(t *testing.T, copyErr error) *[]string {
	t.Helper()
	scripts := &[]string{}
	origRun, origCopy := runBashScript, configureKubeconfig
	runBashScript = func(s []string, _ string) (string, error) {
		*scripts = append(*scripts, s...)
		return "", nil
	}
	configureKubeconfig = func() error { return copyErr }
	t.Cleanup(func() { runBashScript, configureKubeconfig = origRun, origCopy })
	return scripts
}

func hasScript(scripts []string, fragment string) bool {
	for _, s := range scripts {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}

func TestInitializeCluster_FreshInitIsRolledBackWithReset(t *testing.T) {
	stubAPIServer(t, assert.AnError) // no cluster answers: a fresh host
	noControlPlane(t)
	stubVerifyExecutables(t, func(string) error { return nil })
	scripts := stubKubeadm(t, nil)

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.True(t, hasScript(*scripts, "kubeadm init phase preflight"), "preflight must run before init")
	assert.True(t, hasScript(*scripts, "kubeadm init --upload-certs"), "init must run on a fresh host")

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.True(t, hasScript(*scripts, "kubeadm reset --force"), "a cluster this run initialized is reset on rollback")
}

// The record is set as soon as kubeadm init succeeds: a failure in the
// kubeconfig copy still leaves a half-made cluster for rollback to reset.
func TestInitializeCluster_KubeconfigCopyFailureStillResets(t *testing.T) {
	stubAPIServer(t, assert.AnError)
	noControlPlane(t)
	stubVerifyExecutables(t, func(string) error { return nil })
	scripts := stubKubeadm(t, assert.AnError)

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.True(t, hasScript(*scripts, "kubeadm reset --force"))
}

// End to end through the engine: under RollbackOnError a later failure drives
// init-cluster's rollback with the snapshot Execute left, and a skipped init
// stays skipped.
func TestInitializeCluster_ExistingClusterSurvivesWorkflowRollback(t *testing.T) {
	stubAPIServer(t, nil) // existing cluster
	stubKubeconfigCopies(t)
	calls := stubVerifyExecutables(t, func(string) error {
		t.Fatal("no binary may be verified or run on an existing cluster")
		return nil
	})
	failing := automa.NewStepBuilder().WithId("later-step-fails").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.FailureReport(stp, automa.WithError(assert.AnError))
		})

	wf, err := automa.NewWorkflowBuilder().WithId("test-rollback-scope").
		WithExecutionMode(automa.RollbackOnError).
		Steps(InitializeCluster(), failing).
		Build()
	require.NoError(t, err)

	report := wf.Execute(context.Background())
	require.Error(t, report.Error)

	// automa attaches each step's rollback report to that step's own report.
	var initRollback *automa.Report
	for _, sr := range report.StepReports {
		if sr.Id == "init-cluster" {
			initRollback = sr.Rollback
		}
	}
	require.NotNil(t, initRollback, "init-cluster is in the rollback set even though it skipped")
	assert.Equal(t, automa.StatusSkipped, initRollback.Status)
	assert.Empty(t, *calls)
}

// kubeadm init that fails after preflight leaves a half-made control plane
// behind and does not clean it up, so a fresh host's failed init is this run's
// to reset.
func TestInitializeCluster_FailedFreshInitIsReset(t *testing.T) {
	stubAPIServer(t, assert.AnError)
	noControlPlane(t)
	stubVerifyExecutables(t, func(string) error { return nil })
	scripts := stubKubeadm(t, nil)
	runBashScript = func(s []string, _ string) (string, error) {
		*scripts = append(*scripts, s...)
		if hasScript(s, "kubeadm init --upload-certs") {
			return "", assert.AnError
		}
		return "", nil
	}

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	reason, _ := errx.ReasonOf(report.Error)
	assert.Equal(t, ReasonHostCommandFailed, reason)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.True(t, hasScript(*scripts, "kubeadm reset --force"), "a failed init on a fresh host is reset")
}

// A control plane whose API server does not answer is never initialized over:
// the step fails before kubeadm runs and its rollback has nothing to reset.
func TestInitializeCluster_DeadControlPlaneIsNeitherInitializedNorReset(t *testing.T) {
	stubAPIServer(t, assert.AnError)
	leftoverControlPlane(t, "", "admin.conf", "manifests/etcd.yaml")
	calls := stubVerifyExecutables(t, func(string) error { return nil })
	scripts := stubKubeadm(t, nil)

	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	reason, _ := errx.ReasonOf(report.Error)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	assert.Empty(t, *scripts, "kubeadm must not run over an existing control plane")

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Empty(t, *calls)
}
