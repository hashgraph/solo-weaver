// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	config.Init()
	os.Exit(m.Run())
}

// TestKnownSoftwareNames verifies that KnownSoftwareNames returns a non-empty list
// of well-known binary names derived from artifact.yaml.
func TestKnownSoftwareNames(t *testing.T) {
	names := software.KnownSoftwareNames()
	require.NotEmpty(t, names, "KnownSoftwareNames should return at least one entry")

	// Spot-check a few expected names that must always be present.
	expected := []string{"kubectl", "kubelet", "kubeadm", "helm"}
	for _, want := range expected {
		assert.Contains(t, names, want, "expected %q in KnownSoftwareNames", want)
	}

	// No duplicates.
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		_, dup := seen[n]
		assert.False(t, dup, "duplicate software name: %q", n)
		seen[n] = struct{}{}
	}
}

// TestRefreshSoftwareState_EmptySandbox verifies that when no binaries exist on disk,
// every known software is reported as not installed.
func TestRefreshSoftwareState_EmptySandbox(t *testing.T) {
	current := state.NewState()
	checker := &realityChecker{
		current:       current,
		sandboxBinDir: t.TempDir(), // empty temp dir — no binaries present
	}

	sw := checker.refreshSoftwareState()

	require.NotNil(t, sw)
	for name, s := range sw {
		assert.Equal(t, name, s.Name, "Name field should match map key")
		assert.False(t, s.Installed, "%q should be not installed when sandbox is empty", name)
	}
}

// TestRefreshSoftwareState_BinaryPresent verifies that a binary placed in SandboxBinDir
// is reported as installed.
func TestRefreshSoftwareState_BinaryPresent(t *testing.T) {
	tmpDir := t.TempDir()

	names := software.KnownSoftwareNames()
	require.NotEmpty(t, names)
	targetName := names[0]

	binPath := filepath.Join(tmpDir, targetName)
	require.NoError(t, os.WriteFile(binPath, []byte("stub"), 0o755))

	current := state.NewState()
	checker := &realityChecker{
		current:       current,
		sandboxBinDir: tmpDir,
	}

	sw := checker.refreshSoftwareState()

	require.NotNil(t, sw)
	entry, ok := sw[targetName]
	require.True(t, ok, "expected entry for %q in result", targetName)
	assert.True(t, entry.Installed, "%q should be installed when binary is present", targetName)
}

// TestRefreshSoftwareState_PersistedMetadataCarried verifies that version and configured
// fields from the persisted MachineState are carried through to the result.
func TestRefreshSoftwareState_PersistedMetadataCarried(t *testing.T) {
	names := software.KnownSoftwareNames()
	require.NotEmpty(t, names)
	targetName := names[0]

	current := state.NewState()
	current = state.SetSoftwareState(current, targetName, state.SoftwareState{
		Name:       targetName,
		Version:    "1.2.3",
		Configured: true,
	})

	checker := &realityChecker{
		current:       current,
		sandboxBinDir: t.TempDir(), // empty — not testing Installed here
	}

	sw := checker.refreshSoftwareState()

	entry, ok := sw[targetName]
	require.True(t, ok)
	assert.Equal(t, "1.2.3", entry.Version, "version should be carried from new state map")
	assert.True(t, entry.Configured, "configured flag should be carried from new state map")
}

// TestRefreshSoftwareState_LegacySidecarInstalled verifies that when no new-state entry
// exists but a legacy .installed sidecar file is present, the version and Installed flag
// are read correctly from the file.
func TestRefreshSoftwareState_LegacySidecarInstalled(t *testing.T) {
	stateDir := t.TempDir()
	sandboxBinDir := t.TempDir()

	names := software.KnownSoftwareNames()
	require.NotEmpty(t, names)
	targetName := names[0]

	// Write a legacy sidecar file as the installer would.
	sidecarPath := filepath.Join(stateDir, targetName+".installed")
	require.NoError(t, os.WriteFile(sidecarPath, []byte("installed at version 1.30.2\n"), 0o644))

	// Also drop the binary so Installed stays true.
	binPath := filepath.Join(sandboxBinDir, targetName)
	require.NoError(t, os.WriteFile(binPath, []byte("stub"), 0o755))

	// Override stateDir via models.Paths() by injecting it through the stateDir field.
	checker := &realityChecker{
		current:       state.NewState(), // empty new-state map
		sandboxBinDir: sandboxBinDir,
		stateDir:      stateDir,
	}

	sw := checker.refreshSoftwareState()

	entry, ok := sw[targetName]
	require.True(t, ok)
	assert.True(t, entry.Installed, "should be installed — binary present and .installed sidecar exists")
	assert.Equal(t, "1.30.2", entry.Version, "version should be parsed from legacy .installed sidecar")
	assert.False(t, entry.Configured, "configured should be false — no .configured sidecar written")
}

// TestRefreshSoftwareState_LegacySidecarConfigured verifies that the .configured sidecar
// sets Configured=true.
func TestRefreshSoftwareState_LegacySidecarConfigured(t *testing.T) {
	stateDir := t.TempDir()

	names := software.KnownSoftwareNames()
	require.NotEmpty(t, names)
	targetName := names[0]

	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, targetName+".installed"),
		[]byte("installed at version 2.0.0\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, targetName+".configured"),
		[]byte("configured at version 2.0.0\n"), 0o644))

	checker := &realityChecker{
		current:       state.NewState(),
		sandboxBinDir: t.TempDir(), // empty sandbox — binary absent
		stateDir:      stateDir,
	}

	sw := checker.refreshSoftwareState()

	entry, ok := sw[targetName]
	require.True(t, ok)
	assert.False(t, entry.Installed, "binary absent → Installed must be false even if sidecar says installed")
	assert.True(t, entry.Configured, "Configured should be true from .configured sidecar")
	assert.Equal(t, "2.0.0", entry.Version, "version parsed from .installed sidecar")
}

// TestReadLegacySidecarState_ParsesVersion tests the internal sidecar-file parser directly.
func TestReadLegacySidecarState_ParsesVersion(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		content     string
		wantExists  bool
		wantVersion string
	}{
		{"installed at version 1.30.2\n", true, "1.30.2"},
		{"configured at version 0.22.1\n", true, "0.22.1"},
		{"installed at version \n", true, ""}, // empty version after marker
		{"something unexpected\n", true, ""},  // no marker
		{"", true, ""},                        // empty file
	}

	for _, tc := range cases {
		name := "sw"
		sidecarPath := filepath.Join(dir, name+".installed")
		require.NoError(t, os.WriteFile(sidecarPath, []byte(tc.content), 0o644))

		exists, version := readLegacySidecarState(dir, name, "installed")
		assert.True(t, exists, "file exists")
		assert.Equal(t, tc.wantVersion, version, "content: %q", tc.content)

		require.NoError(t, os.Remove(sidecarPath))
	}

	// Non-existent file.
	exists, version := readLegacySidecarState(dir, "missing", "installed")
	assert.False(t, exists)
	assert.Empty(t, version)
}

// TestRefreshHardwareState verifies that refreshHardwareState populates the mandatory
// hardware keys (os, cpu, memory, storage).
func TestRefreshHardwareState(t *testing.T) {
	current := state.NewState()
	checker := &realityChecker{current: current}

	hw := checker.refreshHardwareState()

	require.NotNil(t, hw)

	mandatoryKeys := []string{"os", "cpu", "memory", "storage"}
	for _, key := range mandatoryKeys {
		entry, ok := hw[key]
		require.True(t, ok, "expected hardware key %q", key)
		assert.Equal(t, key, entry.Type, "Type field should match key for %q", key)
		assert.False(t, entry.LastSync.IsZero(), "LastSync should be set for %q", key)
	}
}

// TestMachineState_Integration tests the full MachineState method end-to-end.
func TestMachineState_Integration(t *testing.T) {
	current := state.NewState()
	checker := &realityChecker{
		current:       current,
		sandboxBinDir: t.TempDir(), // hermetic — no root needed
	}

	ms, err := checker.MachineState(context.Background())
	require.NoError(t, err)

	// Software map must contain all known names.
	knownNames := software.KnownSoftwareNames()
	for _, name := range knownNames {
		_, ok := ms.Software[name]
		assert.True(t, ok, "MachineState.Software should contain entry for %q", name)
	}

	// Hardware map must at minimum have os, cpu, memory, storage.
	for _, key := range []string{"os", "cpu", "memory", "storage"} {
		_, ok := ms.Hardware[key]
		assert.True(t, ok, "MachineState.Hardware should contain key %q", key)
	}

	// LastSync should be set.
	assert.False(t, ms.LastSync.IsZero(), "MachineState.LastSync should be set")
}
