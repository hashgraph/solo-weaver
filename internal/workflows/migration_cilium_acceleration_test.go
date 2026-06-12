// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMctx(installed, current string) *migration.Context {
	mctx := &migration.Context{Component: migration.ScopeStartup, Data: &automa.SyncStateBag{}}
	mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, installed)
	mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, current)
	return mctx
}

func TestCiliumAccelerationMigration_Metadata(t *testing.T) {
	m := NewCiliumAccelerationMigration()
	assert.Equal(t, "cilium-disable-xdp-acceleration-v"+ciliumAccelerationMinVersion, m.ID())
	assert.Contains(t, m.Description(), "acceleration=disabled")
}

func TestCiliumAccelerationMigration_Applies(t *testing.T) {
	m := NewCiliumAccelerationMigration()

	tests := []struct {
		name      string
		installed string
		current   string
		want      bool
	}{
		{"upgrade across boundary applies", "0.18.1", "0.19.1", true},
		{"upgrade from below to above applies", "0.18.1", "0.20.0", true},
		{"fresh install does not apply", "", "0.19.1", false},
		{"already past boundary does not apply", "0.19.1", "0.20.0", false},
		{"below boundary on both sides does not apply", "0.18.0", "0.18.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCiliumAccelerationMigration_Execute(t *testing.T) {
	origRead, origReconfigure, origShell := readCiliumAcceleration, reconfigureCiliumConfig, runShell
	t.Cleanup(func() {
		readCiliumAcceleration, reconfigureCiliumConfig, runShell = origRead, origReconfigure, origShell
	})

	m := NewCiliumAccelerationMigration()
	mctx := newMctx("0.18.1", ciliumAccelerationMinVersion)

	// trackUpgrade records whether `cilium upgrade` was shelled out and its command.
	trackUpgrade := func(cmd *string, ran *bool) {
		runShell = func(scripts []string, _ string) (string, error) {
			joined := strings.Join(scripts, " ")
			if strings.Contains(joined, "cilium upgrade") {
				*ran = true
				*cmd = joined
			}
			return "", nil
		}
	}

	t.Run("skips upgrade when acceleration already disabled", func(t *testing.T) {
		var ran bool
		var cmd string
		readCiliumAcceleration = func(context.Context) (string, error) { return "disabled", nil }
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "cilium upgrade must not run when already disabled")
	})

	t.Run("skips upgrade when cilium-config is absent (empty value)", func(t *testing.T) {
		var ran bool
		var cmd string
		readCiliumAcceleration = func(context.Context) (string, error) { return "", nil }
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "cilium upgrade must not run when there is no cilium-config")
	})

	t.Run("no-ops when the cluster is unreachable", func(t *testing.T) {
		var ran bool
		var cmd string
		readCiliumAcceleration = func(context.Context) (string, error) { return "", assert.AnError }
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "cilium upgrade must not run when the cluster is unreachable")
	})

	t.Run("runs cilium upgrade when acceleration is best-effort", func(t *testing.T) {
		var ran bool
		var cmd string
		readCiliumAcceleration = func(context.Context) (string, error) { return "best-effort", nil }
		reconfigureCiliumConfig = func() (string, error) {
			return "/opt/solo/weaver/sandbox/etc/weaver/cilium-config.yaml", nil
		}
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		require.True(t, ran, "cilium upgrade must run when acceleration is best-effort")
		assert.Contains(t, cmd, "cilium upgrade")
		assert.Contains(t, cmd, "--values")
		assert.Contains(t, cmd, "--wait")
	})
}
