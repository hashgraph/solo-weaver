// SPDX-License-Identifier: Apache-2.0

package firewall

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

// TestNftUnitNeedsConverge_Wiring checks the firewall-specific wiring into the
// shared probe. The decision table itself is covered in internal/network/unitconv.
func TestNftUnitNeedsConverge_Wiring(t *testing.T) {

	t.Run("artifact from either plane with no unit gets one", func(t *testing.T) {
		for _, present := range []int{0, 1} {
			dir := t.TempDir()
			artifacts := []string{filepath.Join(dir, "host.nft"), filepath.Join(dir, "policy.nft")}
			require.NoError(t, os.WriteFile(artifacts[present], []byte("table"), 0o644))
			needsConverge, err := nftUnitNeedsConverge(filepath.Join(dir, "unit"), artifacts, enabledIs(true))
			require.NoError(t, err)
			require.True(t, needsConverge, "artifact index %d", present)
		}
	})

	// The probe's baseline is the verbatim embedded unit: a host already on it,
	// enabled, must not be rewritten on every invocation.
	t.Run("the embedded unit, enabled, needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		embedded, err := templates.Files.ReadFile(networkNftServiceTemplate)
		require.NoError(t, err)
		unit := filepath.Join(dir, "unit")
		require.NoError(t, os.WriteFile(unit, embedded, 0o644))
		needsConverge, err := nftUnitNeedsConverge(unit, nil, enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	t.Run("no unit and no artifact needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		needsConverge, err := nftUnitNeedsConverge(filepath.Join(dir, "unit"),
			[]string{filepath.Join(dir, "host.nft")}, enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	// A host still on the pre-#1002 unit must be rewritten, which only holds if
	// the embedded unit is the baseline.
	t.Run("unit from an older release is rewritten", func(t *testing.T) {
		dir := t.TempDir()
		unit := filepath.Join(dir, "unit")
		require.NoError(t, os.WriteFile(unit, []byte("[Unit]\nAfter=local-fs.target\n"), 0o644))
		needsConverge, err := nftUnitNeedsConverge(unit, nil, enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})
}

// TestNetworkNftUnitNeedsConverge_ProductionPaths pins the host paths, so a
// refactor cannot leave the probe pointing somewhere else.
func TestNetworkNftUnitNeedsConverge_ProductionPaths(t *testing.T) {
	// Compare the literals: calling the probe only proves it does not error.
	require.Equal(t, "/usr/lib/systemd/system/solo-provisioner-network-nft.service", NetworkNftServiceUnitPath)
	require.Equal(t, "/etc/solo-provisioner/network-weaver-host-firewall.nft", HostNftPath)
	require.Equal(t, "/etc/solo-provisioner/network-weaver-workload-policy.nft", WeaverNftPath)
}

// TestNftUnitTemplateWantedBy ties the unit to the enablement probe:
// unitconv.EnabledAtBoot looks only in multi-user.target.wants.
func TestNftUnitTemplateWantedBy(t *testing.T) {
	embedded, err := templates.Files.ReadFile(networkNftServiceTemplate)
	require.NoError(t, err)
	// Anchored, not a substring: the template's own comments name the directives
	// asserted on here, so a commented-out one must not satisfy the assertion.
	require.Regexp(t, `(?m)^WantedBy=multi-user\.target$`, string(embedded))
}
