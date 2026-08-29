// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

func ufwConfigured(enabled, known bool) ufwEnabledState {
	return func() (bool, bool) { return enabled, known }
}

func executeFirewallManagersStep(t *testing.T, probe firewallUnitState) *automa.Report {
	t.Helper()
	return executeFirewallManagersStepWithUfw(t, probe, ufwConfigured(true, true))
}

func executeFirewallManagersStepWithUfw(t *testing.T, probe firewallUnitState, ufw ufwEnabledState) *automa.Report {
	t.Helper()
	step, err := checkFirewallManagersStep(probe, ufw).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func ufwActiveOnly(_ context.Context, unit string) (bool, bool, error) {
	return unit == "ufw.service", unit == "ufw.service", nil
}

func TestCheckFirewallManagersStep_PassesOnCleanHost(t *testing.T) {
	report := executeFirewallManagersStep(t, func(context.Context, string) (bool, bool, error) {
		return false, false, nil
	})
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.Empty(t, report.Metadata)
}

func TestCheckFirewallManagersStep_ReportsConflictButPasses(t *testing.T) {
	report := executeFirewallManagersStep(t, func(_ context.Context, unit string) (bool, bool, error) {
		if unit == "ufw.service" {
			return true, true, nil
		}
		return false, true, nil // firewalld running but not enabled
	})
	// Advisory: conflicts are reported but never fail the step.
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.Equal(t, "enabled, running", report.Metadata["ufw.service"])
	require.Equal(t, "running, not enabled", report.Metadata["firewalld.service"])
}

func TestCheckFirewallManagersStep_PassesWhenSystemdUnreachable(t *testing.T) {
	report := executeFirewallManagersStep(t, func(context.Context, string) (bool, bool, error) {
		return false, false, errors.New("no dbus")
	})
	// No systemd on containerized test hosts; must not block the install.
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.Empty(t, report.Metadata)
}

func TestCheckFirewallManagersStep_IgnoresUfwWithDisabledRuleset(t *testing.T) {
	// Stock Ubuntu: ufw.service enabled but ENABLED=no, so no ruleset is loaded.
	report := executeFirewallManagersStepWithUfw(t, ufwActiveOnly, ufwConfigured(false, true))
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.Empty(t, report.Metadata)
}

func TestCheckFirewallManagersStep_ReportsUfwWithEnabledRuleset(t *testing.T) {
	report := executeFirewallManagersStepWithUfw(t, ufwActiveOnly, ufwConfigured(true, true))
	require.NoError(t, report.Error)
	require.Equal(t, "enabled, running", report.Metadata["ufw.service"])
}

func TestCheckFirewallManagersStep_ReportsUfwWhenConfUnreadable(t *testing.T) {
	// Unknown ruleset state falls back to warning.
	report := executeFirewallManagersStepWithUfw(t, ufwActiveOnly, ufwConfigured(false, false))
	require.NoError(t, report.Error)
	require.Equal(t, "enabled, running", report.Metadata["ufw.service"])
}

// TestCheckFirewallManagersStep_ReExecuteDoesNotAccumulate: a step built once
// and run twice must report the same conflict once, not twice.
func TestCheckFirewallManagersStep_ReExecuteDoesNotAccumulate(t *testing.T) {
	step, err := checkFirewallManagersStep(ufwActiveOnly, ufwConfigured(true, true)).Build()
	require.NoError(t, err)

	first := reportedConflicts(step.Execute(context.Background()))
	second := reportedConflicts(step.Execute(context.Background()))

	require.Equal(t, []string{"ufw.service (enabled, running)"}, first)
	require.Equal(t, first, second)
}

// TestReportedConflicts_OrdersByDeclaredManagerList pins the message order: the
// report metadata is a map, so the declared list is what makes it stable.
func TestReportedConflicts_OrdersByDeclaredManagerList(t *testing.T) {
	rpt := &automa.Report{Metadata: automa.StringMap{
		"firewalld.service": "enabled, running",
		"ufw.service":       "running, not enabled",
	}}
	require.Equal(t, []string{
		"ufw.service (running, not enabled)",
		"firewalld.service (enabled, running)",
	}, reportedConflicts(rpt))
}

func TestReportedConflicts_NilReport(t *testing.T) {
	require.Nil(t, reportedConflicts(nil))
}

func constProbe(value bool) func(context.Context, string) (bool, error) {
	return func(context.Context, string) (bool, error) { return value, nil }
}

func failingProbe(msg string) func(context.Context, string) (bool, error) {
	return func(context.Context, string) (bool, error) { return false, errors.New(msg) }
}

func TestCombineUnitProbes_KeepsEnabledWhenRunningQueryFails(t *testing.T) {
	// A failed "running?" query must not discard a known "enabled".
	enabled, running, err := combineUnitProbes(context.Background(), "ufw.service", constProbe(true), failingProbe("no dbus"))
	require.NoError(t, err)
	require.True(t, enabled)
	require.False(t, running)
}

func TestCombineUnitProbes_KeepsRunningWhenEnabledQueryFails(t *testing.T) {
	enabled, running, err := combineUnitProbes(context.Background(), "ufw.service", failingProbe("no dbus"), constProbe(true))
	require.NoError(t, err)
	require.False(t, enabled)
	require.True(t, running)
}

func TestCombineUnitProbes_ErrorsOnlyWhenStateIsFullyUnknown(t *testing.T) {
	// Preserves the container skip path: the step ignores units it cannot probe.
	_, _, err := combineUnitProbes(context.Background(), "ufw.service", failingProbe("no dbus"), failingProbe("no dbus"))
	require.Error(t, err)
}

func TestCombineUnitProbes_PartialFailureWithNoIsStillClean(t *testing.T) {
	// A known "not enabled" plus one failed query stays a pass.
	enabled, running, err := combineUnitProbes(context.Background(), "ufw.service", constProbe(false), failingProbe("no dbus"))
	require.NoError(t, err)
	require.False(t, enabled)
	require.False(t, running)
}

// TestUfwRulesetEnabledAt_Parse covers the ufw.conf cases. The absent and
// malformed ones matter most: both resolve to a known "off", which silences the
// warning.
func TestUfwRulesetEnabledAt_Parse(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantEnabled bool
		wantKnown   bool
	}{
		{"enabled", "ENABLED=yes\n", true, true},
		{"disabled", "ENABLED=no\n", false, true},
		{"uppercase value", "ENABLED=YES\n", true, true},
		{"padded key and value", "  ENABLED = yes \n", true, true},
		{"commented-out setting is not read", "#ENABLED=yes\nENABLED=no\n", false, true},
		{"first setting wins", "ENABLED=no\nENABLED=yes\n", false, true},
		{"other keys are skipped", "IPT_SYSCTL=/etc/ufw/sysctl.conf\nENABLED=yes\n", true, true},
		{"absent key is a known off", "IPT_MODULES=nf_conntrack_ftp\n", false, true},
		{"empty file is a known off", "", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ufw.conf")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o644))

			enabled, known := ufwRulesetEnabledAt(path)
			require.Equal(t, tc.wantKnown, known)
			require.Equal(t, tc.wantEnabled, enabled)
		})
	}
}

// TestUfwRulesetEnabledAt_UnreadableIsUnknown pins that a file we cannot read
// stays unknown, so the step keeps warning instead of silently clearing.
func TestUfwRulesetEnabledAt_UnreadableIsUnknown(t *testing.T) {
	// A directory where the conf belongs: Open succeeds, the read fails.
	dir := filepath.Join(t.TempDir(), "ufw.conf")
	require.NoError(t, os.Mkdir(dir, 0o755))

	enabled, known := ufwRulesetEnabledAt(dir)
	require.False(t, known)
	require.False(t, enabled)
}

// TestUfwConfPath pins the file ufw itself sources; a typo here silently turns
// the check into "state unknown".
func TestUfwConfPath(t *testing.T) {
	require.Equal(t, "/etc/ufw/ufw.conf", ufwConfPath)
}

// TestConflictingFirewallManagers pins the probed set. nftables.service is left
// out on purpose: the loader is ordered after it, so the two coexist and warning
// about it would fire on every host that merely has it installed (#982).
func TestConflictingFirewallManagers(t *testing.T) {
	require.Equal(t, []string{"ufw.service", "firewalld.service"}, conflictingFirewallManagers)
	require.NotContains(t, conflictingFirewallManagers, "nftables.service")
}

func TestUfwRulesetEnabled_MissingConfIsUnknown(t *testing.T) {
	// The real host path: absent on non-Ubuntu and in containers.
	if _, err := os.Stat(ufwConfPath); err == nil {
		t.Skip("host has " + ufwConfPath)
	}
	enabled, known := ufwRulesetEnabled()
	require.False(t, known)
	require.False(t, enabled)
}
