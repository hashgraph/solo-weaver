// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// enabledIs builds an enablement probe with a fixed answer.
func enabledIs(on bool) unitEnabledProbe {
	return func(context.Context) (bool, error) { return on, nil }
}

// TestNftUnitNeedsConverge covers the decision table for whether an
// already-provisioned host has the current loader unit, enabled (#982).
func TestNftUnitNeedsConverge(t *testing.T) {
	ctx := context.Background()
	embedded, err := templates.Files.ReadFile(networkNftServiceTemplate)
	require.NoError(t, err)

	writeFile := func(t *testing.T, path string, content []byte) string {
		t.Helper()
		require.NoError(t, os.WriteFile(path, content, 0o644))
		return path
	}

	t.Run("no unit and no artifact needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		needsConverge, err := nftUnitNeedsConverge(ctx, filepath.Join(dir, "unit"),
			[]string{filepath.Join(dir, "host.nft")}, enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	t.Run("unit matching the embedded copy and enabled needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), embedded)
		needsConverge, err := nftUnitNeedsConverge(ctx, unit,
			[]string{writeFile(t, filepath.Join(dir, "host.nft"), []byte("table"))}, enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	// The #982 case a content compare cannot see: right bytes, still disabled.
	t.Run("unit matching the embedded copy but disabled needs converging", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), embedded)
		needsConverge, err := nftUnitNeedsConverge(ctx, unit, nil, enabledIs(false))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	// A failed query must not read as "enabled" — that is the byte-only compare
	// this branch exists to replace.
	t.Run("an unreadable enablement state is an error, not a silent no-op", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), embedded)
		_, err := nftUnitNeedsConverge(ctx, unit, nil, func(context.Context) (bool, error) {
			return false, errorx.ExternalError.New("no dbus")
		})
		require.Error(t, err)
	})

	// The query costs a DBus round trip, so skip it when there is no unit.
	t.Run("a host with no unit is decided without querying enablement", func(t *testing.T) {
		dir := t.TempDir()
		queried := false
		needsConverge, err := nftUnitNeedsConverge(ctx, filepath.Join(dir, "unit"), nil,
			func(context.Context) (bool, error) { queried = true; return true, nil })
		require.NoError(t, err)
		require.False(t, needsConverge)
		require.False(t, queried, "enablement must only be queried once the unit is known to be current")
	})

	t.Run("unit from an older release is rewritten", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), []byte("[Unit]\nAfter=local-fs.target\n"))
		needsConverge, err := nftUnitNeedsConverge(ctx, unit, nil, enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	t.Run("artifact with no unit gets one", func(t *testing.T) {
		dir := t.TempDir()
		artifact := writeFile(t, filepath.Join(dir, "policy.nft"), []byte("table"))
		needsConverge, err := nftUnitNeedsConverge(ctx, filepath.Join(dir, "unit"),
			[]string{filepath.Join(dir, "host.nft"), artifact}, enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	t.Run("unreadable unit path is an error, not a silent no-op", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where the unit belongs: the read fails, and that must not
		// pass as "already current".
		require.NoError(t, os.Mkdir(filepath.Join(dir, "unit"), 0o755))
		_, err := nftUnitNeedsConverge(ctx, filepath.Join(dir, "unit"), nil, enabledIs(true))
		require.Error(t, err)
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
