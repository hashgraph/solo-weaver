// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAPIDown = errx.Decorate(errorx.ExternalError.Wrap(errors.New("connection refused"), "cannot read kube-system/cilium-config"),
	reasons.K8sAPIFailed, "Check the API server: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz")

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

func TestCiliumAgentRestart_DefersWhenThePatchHitsTheAPI(t *testing.T) {
	stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return true, "disabled", nil }
	restartCiliumAgents = func(context.Context) error { return errAPIDown }

	requireDeferred(t, NewCiliumAgentRestartMigration().Execute(context.Background(), newMctx("0.19.1", "0.35.0")))
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

// A pass where an earlier migration deferred must not roll the agents: the
// next pass would roll them again, on every command until the deferral clears.
func TestCiliumAgentRestart_DefersAfterAnEarlierDeferral(t *testing.T) {
	_, restarted := stubCilium(t)
	readCiliumState = func(context.Context) (bool, string, error) { return true, "disabled", nil }
	readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return true, "false", "false", nil }
	installedCiliumVersion = func(context.Context) (string, error) { return "", errAPIDown }
	mctx := newMctx("0.19.1", "0.35.0")

	wf, err := migration.MigrationsToWorkflow([]migration.Migration{NewCiliumHostLegacyRoutingMigration(), NewCiliumAgentRestartMigration()}, mctx).Build()
	require.NoError(t, err)
	report := wf.Execute(context.Background())

	require.NoError(t, report.Error)
	assert.False(t, *restarted)
	var ids []string
	for _, d := range mctx.Deferred() {
		ids = append(ids, d.ID)
	}
	assert.Equal(t, []string{NewCiliumHostLegacyRoutingMigration().ID(), NewCiliumAgentRestartMigration().ID()}, ids)
	assert.Equal(t, automa.StatusSuccess, report.Status)
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
