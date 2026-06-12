// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCiliumAgentRestartMigration_Metadata(t *testing.T) {
	m := NewCiliumAgentRestartMigration()
	assert.Equal(t, "cilium-agent-restart-disabled-acceleration-v"+ciliumAgentRestartMinVersion, m.ID())
	assert.Contains(t, m.Description(), "Restart Cilium agents")
}

func TestCiliumAgentRestartMigration_Applies(t *testing.T) {
	m := NewCiliumAgentRestartMigration()

	tests := []struct {
		name      string
		installed string
		current   string
		want      bool
	}{
		{"0.19.1 -> 0.19.2 applies (config flipped, agents not restarted)", "0.19.1", "0.19.2", true},
		{"0.18.1 -> 0.19.2 applies", "0.18.1", "0.19.2", true},
		{"fresh install does not apply", "", "0.19.2", false},
		{"already past boundary does not apply", "0.19.2", "0.20.0", false},
		{"below boundary on both sides does not apply", "0.18.1", "0.19.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCiliumAgentRestartMigration_Execute(t *testing.T) {
	origK8s, origState, origRestart := kubernetesInstalled, readCiliumState, restartCiliumAgents
	t.Cleanup(func() {
		kubernetesInstalled, readCiliumState, restartCiliumAgents = origK8s, origState, origRestart
	})

	m := NewCiliumAgentRestartMigration()
	mctx := newMctx("0.19.1", ciliumAgentRestartMinVersion)

	stateReturns := func(installed bool, acc string, err error) {
		readCiliumState = func(context.Context) (bool, string, error) { return installed, acc, err }
	}
	trackRestart := func(restarted *bool) {
		restartCiliumAgents = func(context.Context) error { *restarted = true; return nil }
	}

	t.Run("skips when Kubernetes is not installed", func(t *testing.T) {
		var restarted bool
		kubernetesInstalled = func() bool { return false }
		stateReturns(true, "disabled", nil)
		trackRestart(&restarted)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, restarted)
	})

	// Remaining cases assume Kubernetes is present.
	kubernetesInstalled = func() bool { return true }

	t.Run("skips when Cilium is not installed", func(t *testing.T) {
		var restarted bool
		stateReturns(false, "", nil)
		trackRestart(&restarted)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, restarted)
	})

	t.Run("no-ops when the cluster is unreachable", func(t *testing.T) {
		var restarted bool
		stateReturns(false, "", assert.AnError)
		trackRestart(&restarted)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, restarted)
	})

	t.Run("skips when acceleration is not disabled (config not yet flipped)", func(t *testing.T) {
		var restarted bool
		stateReturns(true, "best-effort", nil)
		trackRestart(&restarted)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, restarted, "restart migration must not act while config is still best-effort")
	})

	t.Run("restarts agents when acceleration is disabled (staged but not applied)", func(t *testing.T) {
		var restarted bool
		stateReturns(true, "disabled", nil)
		trackRestart(&restarted)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.True(t, restarted, "agents must be restarted to apply the disabled config")
	})

	t.Run("fails when the agent restart fails", func(t *testing.T) {
		stateReturns(true, "disabled", nil)
		restartCiliumAgents = func(context.Context) error { return assert.AnError }
		require.Error(t, m.Execute(context.Background(), mctx))
	})
}
