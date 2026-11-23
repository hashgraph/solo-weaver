// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// readError is how the read helpers tag a failed request to the API server.
func readError(cause error) error {
	return errx.Decorate(errorx.ExternalError.Wrap(cause, "cannot read kube-system/cilium-config"),
		reasons.K8sAPIFailed, apiServerHint(),
		"Check the admin credentials: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf auth can-i get configmaps -n kube-system")
}

var (
	errAPIDown   = readError(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	errForbidden = readError(kerrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "cilium-config", errors.New("RBAC")))
)

// stubCilium stubs every Cilium migration seam for a reachable cluster with
// Cilium installed and nothing yet applied, and records what ran.
func stubCilium(t *testing.T) (upgraded, restarted *bool) {
	t.Helper()
	origK8s, origState, origHLR, origVersion, origVerify, origRender, origShell, origRestart :=
		kubernetesInstalled, readCiliumState, readHostLegacyRoutingState, installedCiliumVersion,
		verifyCiliumExecutable, reconfigureCiliumConfig, runShell, restartCiliumAgents
	t.Cleanup(func() {
		kubernetesInstalled, readCiliumState, readHostLegacyRoutingState, installedCiliumVersion,
			verifyCiliumExecutable, reconfigureCiliumConfig, runShell, restartCiliumAgents =
			origK8s, origState, origHLR, origVersion, origVerify, origRender, origShell, origRestart
	})

	upgraded, restarted = new(bool), new(bool)
	kubernetesInstalled = func() bool { return true }
	readCiliumState = func(context.Context) (bool, string, error) { return true, "best-effort", nil }
	readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return true, "false", "false", nil }
	installedCiliumVersion = func(context.Context) (string, error) { return "1.18.1", nil }
	verifyCiliumExecutable = func() error { return nil }
	reconfigureCiliumConfig = func() (string, error) { return "/tmp/cilium-config.yaml", nil }
	runShell = func([]string, string) (string, error) { *upgraded = true; return "", nil }
	restartCiliumAgents = func(context.Context) error { *restarted = true; return nil }
	return upgraded, restarted
}

func requireDeferred(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, migration.ErrDeferred), "want a deferral, got %v", err)
}

func TestCiliumMigrations_DeferWhenTheConfigCannotBeRead(t *testing.T) {
	upgraded, restarted := stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return false, "", errAPIDown }
	readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return false, "", "", errAPIDown }

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumAgentRestartMigration(), NewCiliumHostLegacyRoutingMigration()} {
		requireDeferred(t, m.Execute(context.Background(), newMctx("0.18.1", "0.35.0")))
	}
	assert.False(t, *upgraded)
	assert.False(t, *restarted)
}

func TestCiliumMigrations_DeferWhenTheVersionLookupHitsTheAPI(t *testing.T) {
	upgraded, _ := stubCilium(t)
	installedCiliumVersion = func(context.Context) (string, error) { return "", errAPIDown }

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumHostLegacyRoutingMigration()} {
		requireDeferred(t, m.Execute(context.Background(), newMctx("0.18.1", "0.35.0")))
	}
	assert.False(t, *upgraded)
}

// A 403 or a bad admin kubeconfig won't clear by waiting, so it fails the
// command instead of deferring it.
func TestCiliumMigrations_FailWhenTheAPIRefusesTheRead(t *testing.T) {
	upgraded, restarted := stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return false, "", errForbidden }
	readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return false, "", "", errForbidden }

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumAgentRestartMigration(), NewCiliumHostLegacyRoutingMigration()} {
		err := m.Execute(context.Background(), newMctx("0.18.1", "0.35.0"))
		require.Error(t, err, m.ID())
		assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "%s: a 403 must fail, got %v", m.ID(), err)
	}
	assert.False(t, *upgraded)
	assert.False(t, *restarted)
}

func TestCiliumMigrations_FailWhenTheVersionLookupIsRefused(t *testing.T) {
	upgraded, _ := stubCilium(t)
	installedCiliumVersion = func(context.Context) (string, error) { return "", errForbidden }

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumHostLegacyRoutingMigration()} {
		err := m.Execute(context.Background(), newMctx("0.18.1", "0.35.0"))
		require.Error(t, err, m.ID())
		assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "%s: a 403 must fail, got %v", m.ID(), err)
	}
	assert.False(t, *upgraded)
}

// A deferral means the API could not be reached, so its warning names only
// the API server check, not the credentials one.
func TestCiliumMigrations_DeferralHintsNameOnlyTheAPIServer(t *testing.T) {
	stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return false, "", errAPIDown }
	mctx := newMctx("0.18.1", "0.35.0")

	wf, err := migration.MigrationsToWorkflow([]migration.Migration{NewCiliumAccelerationMigration()}, mctx).Build()
	require.NoError(t, err)
	require.NoError(t, wf.Execute(context.Background()).Error)

	deferred := mctx.Deferred()
	require.Len(t, deferred, 1)
	assert.Equal(t, []string{apiServerHint()}, deferred[0].Hints)
}

func TestCiliumMigrations_FailWhenTheVersionCannotBeTrusted(t *testing.T) {
	upgraded, _ := stubCilium(t)
	installedCiliumVersion = func(context.Context) (string, error) {
		return "", errx.Decorate(errorx.IllegalState.New("agent 1.18.1 does not match chart 1.20.2"), reasons.PreconditionNotMet)
	}

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumHostLegacyRoutingMigration()} {
		err := m.Execute(context.Background(), newMctx("0.18.1", "0.35.0"))
		require.Error(t, err)
		assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "a deliberate refusal never clears by waiting")
	}
	assert.False(t, *upgraded)
}

// A corrupt Helm release secret ends in io.ErrUnexpectedEOF, which reads like
// a dropped connection; it is a local decode error, so it fails.
func TestCiliumMigrations_FailOnACorruptReleaseSecret(t *testing.T) {
	upgraded, _ := stubCilium(t)
	installedCiliumVersion = func(context.Context) (string, error) {
		return "", errx.Decorate(errorx.IllegalFormat.Wrap(io.ErrUnexpectedEOF, "release payload is not valid gzip"), reasons.PreconditionNotMet)
	}

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumHostLegacyRoutingMigration()} {
		err := m.Execute(context.Background(), newMctx("0.18.1", "0.35.0"))
		require.Error(t, err)
		assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "%s: got %v", m.ID(), err)
		reason, _ := errx.ReasonOf(err)
		assert.Equal(t, reasons.PreconditionNotMet, reason, "%s: got %v", m.ID(), err)
	}
	assert.False(t, *upgraded)
}

// cilium upgrade may have changed the cluster before it lost the API, so its
// failure never defers.
func TestCiliumMigrations_FailWhenTheUpgradeLosesTheAPI(t *testing.T) {
	stubCilium(t)
	runShell = func([]string, string) (string, error) { return "", errAPIDown }

	for _, m := range []migration.Migration{NewCiliumAccelerationMigration(), NewCiliumHostLegacyRoutingMigration()} {
		err := m.Execute(context.Background(), newMctx("0.18.1", "0.35.0"))
		require.Error(t, err)
		assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "%s: got %v", m.ID(), err)
	}
}

// The patch may have landed before the connection dropped, so the restart
// fails rather than defers: a deferral promises nothing changed.
func TestCiliumAgentRestart_FailsWhenThePatchCannotReachTheAPI(t *testing.T) {
	stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return true, "disabled", nil }
	restartCiliumAgents = func(context.Context) error { return errAPIDown }

	err := NewCiliumAgentRestartMigration().Execute(context.Background(), newMctx("0.19.1", "0.35.0"))

	require.Error(t, err)
	assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "got %v", err)
}

func TestCiliumAgentRestart_FailsWhenTheRolloutDoesNotFinish(t *testing.T) {
	stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return true, "disabled", nil }
	restartCiliumAgents = func(context.Context) error {
		return errorx.TimeoutElapsed.New("rollout of daemonset kube-system/cilium did not finish")
	}

	err := NewCiliumAgentRestartMigration().Execute(context.Background(), newMctx("0.19.1", "0.35.0"))

	require.Error(t, err)
	assert.False(t, errorx.IsOfType(err, migration.ErrDeferred), "the agents were already patched")
}

// The cilium CLI must use the same admin kubeconfig as the reads that let the
// migration run, not root's ~/.kube/config, which can be missing on a healthy
// cluster.
func TestCiliumMigrations_UpgradeUsesTheAdminKubeconfig(t *testing.T) {
	for _, tc := range pinCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newPinHarness(t)
			tc.needsRun()
			installedCiliumVersion = func(context.Context) (string, error) { return "1.18.1", nil }

			require.NoError(t, tc.m.Execute(context.Background(), tc.mctx))

			require.Len(t, h.upgrades, 1)
			assert.Contains(t, h.upgrades[0], "cilium upgrade --kubeconfig /etc/kubernetes/admin.conf ")
		})
	}
}
