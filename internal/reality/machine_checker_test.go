// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineChecker_MachineState_UsesPersisted(t *testing.T) {
	sm := &fakeStateManager{
		state: state.State{
			MachineState: state.MachineState{
				Software: map[string]state.SoftwareState{
					"kubectl": {Name: "kubectl", Installed: true, Version: "v1.30.0"},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	m := newMachineChecker(sm, tmpDir, tmpDir)

	ms, err := m.MachineState(context.Background())
	require.NoError(t, err)

	sw, ok := ms.Software["kubectl"]
	require.True(t, ok, "kubectl should be present in software state")
	// kubectl binary is NOT in tmpDir so live check forces Installed=false
	assert.False(t, sw.Installed, "Installed should be false when binary absent from sandboxBinDir")
	// Version carries over from persisted state (live check only changes Installed)
	assert.Equal(t, "v1.30.0", sw.Version)
}

func TestMachineChecker_MachineState_LiveBinaryPresent(t *testing.T) {
	sm := &fakeStateManager{state: state.NewState()}

	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "kubectl"), []byte("fake"), 0755)
	require.NoError(t, err)

	m := newMachineChecker(sm, tmpDir, tmpDir)
	ms, err := m.MachineState(context.Background())
	require.NoError(t, err)

	sw, ok := ms.Software["kubectl"]
	require.True(t, ok)
	assert.True(t, sw.Installed, "Installed should be true when binary is present on disk")
}

func TestMachineChecker_MachineState_HasHardware(t *testing.T) {
	sm := &fakeStateManager{state: state.NewState()}
	m := newMachineChecker(sm, t.TempDir(), t.TempDir())

	ms, err := m.MachineState(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, ms.Hardware, "Hardware map should be populated")

	_, hasCPU := ms.Hardware["cpu"]
	assert.True(t, hasCPU, "cpu entry should be present")

	_, hasMemory := ms.Hardware["memory"]
	assert.True(t, hasMemory, "memory entry should be present")
}

func TestMachineChecker_ReadLegacyStateFiles(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(
		filepath.Join(tmpDir, "kubectl.installed"),
		[]byte("installed at version v1.29.0\n"),
		0644,
	)
	require.NoError(t, err)

	exists, version := readLegacyStateFiles(tmpDir, "kubectl", "installed")
	assert.True(t, exists)
	assert.Equal(t, "v1.29.0", version)
}

func TestMachineChecker_ReadLegacyStateFiles_Missing(t *testing.T) {
	exists, version := readLegacyStateFiles(t.TempDir(), "unknown-tool", "installed")
	assert.False(t, exists)
	assert.Empty(t, version)
}
