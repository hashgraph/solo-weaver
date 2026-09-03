// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/unitconv"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/stretchr/testify/require"
)

// enabledIs builds an enablement probe with a fixed answer.
func enabledIs(on bool) unitconv.EnabledProbe {
	return func() (bool, error) { return on, nil }
}

// TestTcEgressUnitNeedsConverge_Wiring checks the shaper-specific wiring into the
// shared probe. The decision table itself is covered in internal/network/unitconv.
func TestTcEgressUnitNeedsConverge_Wiring(t *testing.T) {

	t.Run("boot script with no unit gets one", func(t *testing.T) {
		dir := t.TempDir()
		script := filepath.Join(dir, "shaper.sh")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755))
		needsConverge, err := tcEgressUnitNeedsConverge(filepath.Join(dir, "unit"), script, enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	// The probe's baseline is the verbatim embedded unit: a host already on it,
	// enabled, must not be rewritten on every invocation.
	t.Run("the embedded unit, enabled, needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		embedded, err := templates.Files.ReadFile(tcEgressServiceTemplate)
		require.NoError(t, err)
		unit := filepath.Join(dir, "unit")
		require.NoError(t, os.WriteFile(unit, embedded, 0o644))
		needsConverge, err := tcEgressUnitNeedsConverge(unit, filepath.Join(dir, "shaper.sh"), enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	t.Run("no unit and no boot script needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		needsConverge, err := tcEgressUnitNeedsConverge(filepath.Join(dir, "unit"),
			filepath.Join(dir, "shaper.sh"), enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	// A host still on the pre-#980 unit must be rewritten, which only holds if the
	// embedded unit is the baseline.
	t.Run("unit from an older release is rewritten", func(t *testing.T) {
		dir := t.TempDir()
		unit := filepath.Join(dir, "unit")
		require.NoError(t, os.WriteFile(unit, []byte(
			"[Unit]\nDefaultDependencies=no\nAfter=network-pre.target\nBefore=network.target\n"), 0o644))
		needsConverge, err := tcEgressUnitNeedsConverge(unit, filepath.Join(dir, "shaper.sh"), enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})
}

// TestTcEgressUnitNeedsConverge_ProductionPaths pins the host paths, so a refactor
// cannot leave the probe pointing somewhere else.
func TestTcEgressUnitNeedsConverge_ProductionPaths(t *testing.T) {
	// Compare literals: calling the probe only proves it does not error.
	require.Equal(t, "/usr/lib/systemd/system/solo-provisioner-bandwidth-shaper.service", TcEgressServiceUnitPath)
	require.Equal(t, "/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh", TcEgressScriptPath)
}

// TestTcEgressUnitTemplateWantedBy ties the unit to the enablement probe:
// unitconv.EnabledAtBoot looks only in multi-user.target.wants.
func TestTcEgressUnitTemplateWantedBy(t *testing.T) {
	embedded, err := templates.Files.ReadFile(tcEgressServiceTemplate)
	require.NoError(t, err)
	requireDirective(t, string(embedded), "Install", "WantedBy=multi-user.target")
}
