// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/migration"
)

// pinCase drives one Cilium migration into the branch that runs `cilium upgrade`.
type pinCase struct {
	name     string
	m        migration.Migration
	mctx     *migration.Context
	needsRun func()
}

// pinCases returns both migrations that run `cilium upgrade`, each with its
// live-state stub set so the upgrade is needed.
func pinCases() []pinCase {
	return []pinCase{
		{
			name: "acceleration",
			m:    NewCiliumAccelerationMigration(),
			mctx: newMctx("0.18.1", ciliumAccelerationMinVersion),
			needsRun: func() {
				readCiliumState = func(context.Context) (bool, string, error) { return true, "best-effort", nil }
			},
		},
		{
			name: "host-legacy-routing",
			m:    NewCiliumHostLegacyRoutingMigration(),
			mctx: newMctx("0.20.0", ciliumHostLegacyRoutingMinVersion),
			needsRun: func() {
				readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return true, "false", "false", nil }
			},
		},
	}
}

// pinHarness stubs every seam the migrations use and records what they did.
type pinHarness struct {
	upgrades    []string
	reconfigure int
	verify      int
}

func newPinHarness(t *testing.T) *pinHarness {
	t.Helper()
	origK8s, origState, origHLR := kubernetesInstalled, readCiliumState, readHostLegacyRoutingState
	origReconfigure, origShell, origVerify, origVersion := reconfigureCiliumConfig, runShell, verifyCiliumExecutable, installedCiliumVersion
	t.Cleanup(func() {
		kubernetesInstalled, readCiliumState, readHostLegacyRoutingState = origK8s, origState, origHLR
		reconfigureCiliumConfig, runShell, verifyCiliumExecutable, installedCiliumVersion = origReconfigure, origShell, origVerify, origVersion
	})

	h := &pinHarness{}
	kubernetesInstalled = func() bool { return true }
	reconfigureCiliumConfig = func() (string, error) {
		h.reconfigure++
		return "/opt/solo/weaver/sandbox/etc/weaver/cilium-config.yaml", nil
	}
	verifyCiliumExecutable = func() error {
		h.verify++
		return nil
	}
	runShell = func(scripts []string, _ string) (string, error) {
		if joined := strings.Join(scripts, " "); strings.Contains(joined, "cilium upgrade") {
			h.upgrades = append(h.upgrades, joined)
		}
		return "", nil
	}
	return h
}

func TestCiliumMigrations_PinUpgradeToDeployedVersion(t *testing.T) {
	for _, tc := range pinCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newPinHarness(t)
			tc.needsRun()
			// An existing cluster deployed by an older provisioner: the live
			// release is 1.18.1 while new installs default to 1.20.2.
			installedCiliumVersion = func(context.Context) (string, error) { return "1.18.1", nil }

			require.NoError(t, tc.m.Execute(context.Background(), tc.mctx))

			require.Len(t, h.upgrades, 1)
			assert.Contains(t, h.upgrades[0], `cilium upgrade --version "1.18.1" --values `)
			assert.NotContains(t, h.upgrades[0], "1.20")
			assert.Contains(t, h.upgrades[0], "--wait")
		})
	}
}

func TestCiliumMigrations_NoUpgradeWithoutDeployedVersion(t *testing.T) {
	for _, tc := range pinCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newPinHarness(t)
			tc.needsRun()
			installedCiliumVersion = func(context.Context) (string, error) { return "", assert.AnError }

			err := tc.m.Execute(context.Background(), tc.mctx)

			require.ErrorIs(t, err, assert.AnError)
			assert.Contains(t, err.Error(), "'cilium upgrade' was not run")
			assert.Empty(t, h.upgrades, "never run an unpinned upgrade")
			assert.Zero(t, h.reconfigure, "cilium-config.yaml must not be re-rendered when the upgrade cannot run")
			assert.Zero(t, h.verify, "the cilium binary is not needed when the upgrade cannot run")
		})
	}
}

func TestCiliumMigrations_VerifyFailureLeavesConfigUntouched(t *testing.T) {
	for _, tc := range pinCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newPinHarness(t)
			tc.needsRun()
			installedCiliumVersion = func(context.Context) (string, error) { return "1.18.1", nil }
			verifyCiliumExecutable = func() error { return assert.AnError }

			require.ErrorIs(t, tc.m.Execute(context.Background(), tc.mctx), assert.AnError)
			assert.Empty(t, h.upgrades)
			assert.Zero(t, h.reconfigure)
		})
	}
}

func TestCiliumMigrations_GuardsSkipVersionLookup(t *testing.T) {
	h := newPinHarness(t)
	lookups := 0
	installedCiliumVersion = func(context.Context) (string, error) {
		lookups++
		return "1.18.1", nil
	}

	readCiliumState = func(context.Context) (bool, string, error) { return true, disabledAcceleration, nil }
	require.NoError(t, NewCiliumAccelerationMigration().Execute(context.Background(), newMctx("0.18.1", ciliumAccelerationMinVersion)))

	readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return true, "true", "false", nil }
	require.NoError(t, NewCiliumHostLegacyRoutingMigration().Execute(context.Background(), newMctx("0.20.0", ciliumHostLegacyRoutingMinVersion)))

	readHostLegacyRoutingState = func(context.Context) (bool, string, string, error) { return true, "false", "true", nil }
	require.Error(t, NewCiliumHostLegacyRoutingMigration().Execute(context.Background(), newMctx("0.20.0", ciliumHostLegacyRoutingMinVersion)),
		"the Bandwidth Manager guard still fails first")

	assert.Zero(t, lookups, "a migration with nothing to do must not need the live version")
	assert.Empty(t, h.upgrades)
}
