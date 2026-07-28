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

// TestStartupPersist_AgentRestartRunsOnceAcrossRuns: the real restart migration fires once across two runs because run 1 records the version (#789/#781). Linux-only: `task vm:test:unit`.
func TestStartupPersist_AgentRestartRunsOnceAcrossRuns(t *testing.T) {
	origK8s, origRead, origRestart := kubernetesInstalled, readCiliumState, restartCiliumAgents
	t.Cleanup(func() {
		kubernetesInstalled, readCiliumState, restartCiliumAgents = origK8s, origRead, origRestart
	})

	// Acceleration already "disabled": Execute() has no "already restarted" guard,
	// so it restarts every time it runs — the version gate is what stops repeats.
	kubernetesInstalled = func() bool { return true }
	readCiliumState = func(_ context.Context) (bool, string, error) {
		return true, disabledAcceleration, nil
	}
	restarts := 0
	restartCiliumAgents = func(_ context.Context) error { restarts++; return nil }

	const currentCLIVersion = "0.21.0" // >= the 0.19.2 restart boundary

	m := NewCiliumAgentRestartMigration()

	// runStartupPass mirrors one RunStartupMigrations invocation: resolve version, check Applies(), Execute() if so.
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

	// Run 1 — no state.yaml → "" → 0.0.0 baseline → boundary applies → restart.
	// RunStartupMigrations then persists currentCLIVersion.
	runStartupPass("")
	assert.Equal(t, 1, restarts, "first run on a pre-state-tracking host restarts the agents once")

	// Run 2 — the recorded version is read back: boundary no longer applies, so the
	// restart is skipped. Without the persist it would still be "" and restart again.
	runStartupPass(currentCLIVersion)
	assert.Equal(t, 1, restarts, "second run must not restart the agents again")
}
