// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCiliumHostLegacyRoutingMigration_Metadata(t *testing.T) {
	m := NewCiliumHostLegacyRoutingMigration()
	assert.Equal(t, "cilium-host-legacy-routing-v"+ciliumHostLegacyRoutingMinVersion, m.ID())
	assert.Contains(t, m.Description(), "enable-host-legacy-routing=true")
}

func TestCiliumHostLegacyRoutingMigration_Applies(t *testing.T) {
	m := NewCiliumHostLegacyRoutingMigration()

	tests := []struct {
		name      string
		installed string
		current   string
		want      bool
	}{
		{"upgrade across boundary applies", "0.20.0", ciliumHostLegacyRoutingMinVersion, true},
		{"upgrade from below to above applies", "0.19.2", "0.22.0", true},
		{"fresh install does not apply", "", ciliumHostLegacyRoutingMinVersion, false},
		{"already past boundary does not apply", ciliumHostLegacyRoutingMinVersion, "0.22.0", false},
		{"below boundary on both sides does not apply", "0.19.0", "0.20.0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCiliumHostLegacyRoutingMigration_Execute(t *testing.T) {
	origK8s := kubernetesInstalled
	origState := readHostLegacyRoutingState
	origReconfigure := reconfigureCiliumConfig
	origShell := runShell
	origVerify := verifyCiliumExecutable
	t.Cleanup(func() {
		kubernetesInstalled = origK8s
		readHostLegacyRoutingState = origState
		reconfigureCiliumConfig = origReconfigure
		runShell = origShell
		verifyCiliumExecutable = origVerify
	})

	m := NewCiliumHostLegacyRoutingMigration()
	mctx := newMctx("0.20.0", ciliumHostLegacyRoutingMinVersion)

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
	stateReturns := func(installed bool, hlr, bm string, err error) {
		readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) {
			return installed, hlr, bm, err
		}
	}

	t.Run("skips when Kubernetes is not installed", func(t *testing.T) {
		var ran bool
		var cmd string
		kubernetesInstalled = func() bool { return false }
		stateReturns(true, "", "", nil)
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "must not run before Kubernetes is installed")
	})

	// Remaining cases assume Kubernetes is present and the cilium binary passes
	// checksum verification unless a case overrides it.
	kubernetesInstalled = func() bool { return true }
	verifyCiliumExecutable = func() error { return nil }

	t.Run("skips when Cilium is not installed", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(false, "", "", nil)
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "must not run before Cilium is installed")
	})

	t.Run("no-ops when the cluster is unreachable", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(false, "", "", assert.AnError)
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "must not run when the cluster is unreachable")
	})

	t.Run("skips upgrade when host-legacy-routing already true", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "true", "", nil)
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "cilium upgrade must not run when already enabled")
	})

	t.Run("skips upgrade when host-legacy-routing already True (case-insensitive)", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "True", "", nil)
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "cilium upgrade must not run when already enabled (case-insensitive)")
	})

	t.Run("fails fast when Bandwidth Manager is enabled", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "", "true", nil)
		trackUpgrade(&cmd, &ran)
		err := m.Execute(context.Background(), mctx)
		require.Error(t, err, "must fail fast when Bandwidth Manager is enabled")
		assert.Contains(t, err.Error(), "Bandwidth Manager is enabled")
		assert.False(t, ran, "cilium upgrade must not run when Bandwidth Manager is enabled")
	})

	t.Run("fails fast when Bandwidth Manager is enabled even if host-legacy-routing is already true", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "true", "true", nil)
		trackUpgrade(&cmd, &ran)
		err := m.Execute(context.Background(), mctx)
		require.Error(t, err, "must fail fast when Bandwidth Manager is enabled")
		assert.Contains(t, err.Error(), "Bandwidth Manager is enabled")
		assert.False(t, ran, "cilium upgrade must not run when Bandwidth Manager is enabled")
	})

	t.Run("runs cilium upgrade when host-legacy-routing is absent", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "", "", nil)
		reconfigureCiliumConfig = func() (string, error) {
			return "/opt/solo/weaver/sandbox/etc/weaver/cilium-config.yaml", nil
		}
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		require.True(t, ran, "cilium upgrade must run when host-legacy-routing is absent")
		assert.Contains(t, cmd, "cilium upgrade")
		assert.Contains(t, cmd, "--values")
		assert.Contains(t, cmd, "--wait")
	})

	t.Run("runs cilium upgrade when host-legacy-routing is false", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "false", "", nil)
		reconfigureCiliumConfig = func() (string, error) {
			return "/opt/solo/weaver/sandbox/etc/weaver/cilium-config.yaml", nil
		}
		trackUpgrade(&cmd, &ran)
		require.NoError(t, m.Execute(context.Background(), mctx))
		require.True(t, ran, "cilium upgrade must run when host-legacy-routing is false")
		assert.Contains(t, cmd, "cilium upgrade")
		assert.Contains(t, cmd, "--values")
		assert.Contains(t, cmd, "--wait")
	})

	t.Run("aborts before upgrade when the cilium binary fails verification", func(t *testing.T) {
		var ran bool
		var cmd string
		stateReturns(true, "", "", nil)
		reconfigureCiliumConfig = func() (string, error) {
			return "/opt/solo/weaver/sandbox/etc/weaver/cilium-config.yaml", nil
		}
		trackUpgrade(&cmd, &ran)
		verifyCiliumExecutable = func() error { return assert.AnError }
		t.Cleanup(func() { verifyCiliumExecutable = func() error { return nil } })

		require.Error(t, m.Execute(context.Background(), mctx))
		assert.False(t, ran, "cilium upgrade must not run when the binary fails checksum verification")
	})
}
