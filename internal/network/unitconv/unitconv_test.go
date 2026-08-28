// SPDX-License-Identifier: Apache-2.0

package unitconv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// embeddedUnit stands in for any plane's embedded unit; NeedsConverge only compares
// bytes.
var embeddedUnit = []byte("[Unit]\nAfter=network-online.target\n")

// enabledIs builds an enablement probe with a fixed answer.
func enabledIs(on bool) EnabledProbe {
	return func() (bool, error) { return on, nil }
}

func writeFile(t *testing.T, path string, content []byte) string {
	t.Helper()
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

// TestNeedsConverge covers the decision table for whether an already-provisioned
// host has the current unit, enabled (#980, #982).
func TestNeedsConverge(t *testing.T) {

	t.Run("no unit and no guard needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		needsConverge, err := NeedsConverge(filepath.Join(dir, "unit"), embeddedUnit,
			[]string{filepath.Join(dir, "artifact")}, enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	t.Run("unit matching the embedded copy and enabled needs nothing", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), embeddedUnit)
		needsConverge, err := NeedsConverge(unit, embeddedUnit,
			[]string{writeFile(t, filepath.Join(dir, "artifact"), []byte("x"))}, enabledIs(true))
		require.NoError(t, err)
		require.False(t, needsConverge)
	})

	// The case a content compare cannot see: right bytes, still disabled.
	t.Run("unit matching the embedded copy but disabled needs converging", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), embeddedUnit)
		needsConverge, err := NeedsConverge(unit, embeddedUnit, nil, enabledIs(false))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	// A failed query must not read as "enabled".
	t.Run("an unreadable enablement state is an error, not a silent no-op", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), embeddedUnit)
		_, err := NeedsConverge(unit, embeddedUnit, nil, func() (bool, error) {
			return false, errors.New("unreadable")
		})
		require.Error(t, err)
	})

	// A host with no unit has nothing to be enabled; skip the probe entirely.
	t.Run("a host with no unit is decided without querying enablement", func(t *testing.T) {
		dir := t.TempDir()
		queried := false
		needsConverge, err := NeedsConverge(filepath.Join(dir, "unit"), embeddedUnit, nil,
			func() (bool, error) { queried = true; return true, nil })
		require.NoError(t, err)
		require.False(t, needsConverge)
		require.False(t, queried, "enablement must only be queried once the unit is known to be current")
	})

	t.Run("unit from an older release is rewritten", func(t *testing.T) {
		dir := t.TempDir()
		unit := writeFile(t, filepath.Join(dir, "unit"), []byte("[Unit]\nAfter=local-fs.target\n"))
		needsConverge, err := NeedsConverge(unit, embeddedUnit, nil, enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	// Any one guard is enough, whichever plane persisted it.
	t.Run("a guard with no unit gets one", func(t *testing.T) {
		dir := t.TempDir()
		artifact := writeFile(t, filepath.Join(dir, "second"), []byte("x"))
		needsConverge, err := NeedsConverge(filepath.Join(dir, "unit"), embeddedUnit,
			[]string{filepath.Join(dir, "first"), artifact}, enabledIs(true))
		require.NoError(t, err)
		require.True(t, needsConverge)
	})

	// A guard whose parent is a regular file: the ENOTDIR must surface, not read as
	// "nothing persisted".
	t.Run("unreadable guard path is an error, not a silent no-op", func(t *testing.T) {
		dir := t.TempDir()
		notADir := writeFile(t, filepath.Join(dir, "file"), []byte("x"))
		_, err := NeedsConverge(filepath.Join(dir, "unit"), embeddedUnit,
			[]string{filepath.Join(notADir, "artifact")}, enabledIs(true))
		require.Error(t, err)
	})

	t.Run("unreadable unit path is an error, not a silent no-op", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where the unit belongs: the read fails, and a failed read must
		// not pass as "already current".
		require.NoError(t, os.Mkdir(filepath.Join(dir, "unit"), 0o755))
		_, err := NeedsConverge(filepath.Join(dir, "unit"), embeddedUnit, nil, enabledIs(true))
		require.Error(t, err)
	})
}
