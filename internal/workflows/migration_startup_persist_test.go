// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartupPersist_AgentRestartRunsOnceAcrossRuns is the end-to-end proof for
// #789/#781: it drives the REAL, non-idempotent CiliumAgentRestartMigration the
// way RunStartupMigrations does, across two invocations, and shows the disruptive
// agent restart fires exactly once — because the first run records the CLI
// version, so the second run no longer crosses the boundary.
//
// Runs under `task vm:test:unit` (the workflows package is Linux-only).
func TestStartupPersist_AgentRestartRunsOnceAcrossRuns(t *testing.T) {
	origK8s, origRead, origRestart := kubernetesInstalled, readCiliumState, restartCiliumAgents
	t.Cleanup(func() {
		kubernetesInstalled, readCiliumState, restartCiliumAgents = origK8s, origRead, origRestart
	})

	// A provisioned cluster whose acceleration is already "disabled": the restart
	// migration's Execute() has no "already restarted" guard, so it WILL restart
	// the agents every time it is allowed to run. The version gate is the only
	// thing that stops repeated restarts.
	kubernetesInstalled = func() bool { return true }
	readCiliumState = func(_ context.Context) (bool, string, error) {
		return true, disabledAcceleration, nil
	}
	restarts := 0
	restartCiliumAgents = func(_ context.Context) error { restarts++; return nil }

	const currentCLIVersion = "0.21.0" // >= the 0.19.2 restart boundary

	m := NewCiliumAgentRestartMigration()

	// runStartupPass mirrors RunStartupMigrations for one invocation: resolve the
	// on-disk version, evaluate Applies(), and Execute() only if it applies.
	runStartupPass := func(onDiskCLIVersion string) {
		mctx := &migration.Context{Data: &automa.SyncStateBag{}}
		mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, migration.ResolveInstalledCLIVersion(onDiskCLIVersion))
		mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, currentCLIVersion)

		applies, err := m.Applies(mctx)
		require.NoError(t, err)
		if applies {
			require.NoError(t, m.Execute(context.Background(), mctx))
		}
	}

	// Run 1 — pre-state-tracking host: no state.yaml → "" → 0.0.0 baseline → the
	// boundary applies → the agents are restarted. RunStartupMigrations then
	// persists currentCLIVersion.
	runStartupPass("")
	assert.Equal(t, 1, restarts, "first run on a pre-state-tracking host restarts the agents once")

	// Run 2 — the recorded version is read back: the boundary no longer applies,
	// so Execute() (and the restart) is skipped. Without the persisted version the
	// on-disk value would still be "" and this would restart the agents again.
	runStartupPass(currentCLIVersion)
	assert.Equal(t, 1, restarts, "second run must not restart the agents again")
}
