// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// shapingOnLegacyYAML is a state.yaml for a node with shaping on, written
// before trafficShapingEnabled existed: no opt-out key, a shaping record.
const shapingOnLegacyYAML = `
hash: abc123
state:
    version: v2
    blockNodeState:
        name: block-node
        namespace: block-node
        shaping:
            egressInterface: eth0
            linkRate: 1gbit
`

func blockNodeStateOf(t *testing.T, b []byte) BlockNodeState {
	t.Helper()
	var doc struct {
		State struct {
			BlockNodeState BlockNodeState `yaml:"blockNodeState"`
		} `yaml:"state"`
	}
	require.NoError(t, yaml.Unmarshal(b, &doc))
	return doc.State.BlockNodeState
}

func TestTrafficShapingEnabledV1Migration_Metadata(t *testing.T) {
	m := NewTrafficShapingEnabledV1Migration()
	assert.Equal(t, "traffic-shaping-enabled-v1", m.ID())
	assert.Contains(t, m.Description(), "trafficShapingEnabled")
	assert.Contains(t, m.stateFilePath(), StateFileName)
}

func TestAddTrafficShapingEnabled(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          string
		wantApplies bool
		wantEnabled bool
	}{
		{
			name:        "shaping record without opt-out is enabled",
			in:          shapingOnLegacyYAML,
			wantApplies: true,
			wantEnabled: true,
		},
		{
			name: "opt-out with a kept shaping record stays off",
			in: `
state:
    blockNodeState:
        trafficShapingDisabled: true
        shaping:
            egressInterface: eth0
`,
		},
		{
			name: "opt-out without shaping stays off",
			in: `
state:
    blockNodeState:
        trafficShapingDisabled: true
`,
		},
		{
			name: "no record at all stays off",
			in: `
state:
    blockNodeState:
        name: block-node
`,
		},
		{
			name: "null shaping stays off",
			in: `
state:
    blockNodeState:
        shaping: null
`,
		},
		{
			name: "already migrated is left alone",
			in: `
state:
    blockNodeState:
        trafficShapingEnabled: true
        shaping:
            egressInterface: eth0
`,
			wantEnabled: true,
		},
		{
			name: "no blockNodeState",
			in: `
state:
    version: v2
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantApplies, needsTrafficShapingEnabled([]byte(tc.in)))

			out, err := addTrafficShapingEnabled([]byte(tc.in))
			require.NoError(t, err)
			assert.Equal(t, tc.wantEnabled, blockNodeStateOf(t, out).TrafficShapingEnabled)
			assert.False(t, needsTrafficShapingEnabled(out), "must not apply twice")
		})
	}
}

func TestAddTrafficShapingEnabled_KeepsOtherFields(t *testing.T) {
	out, err := addTrafficShapingEnabled([]byte(shapingOnLegacyYAML))
	require.NoError(t, err)

	bn := blockNodeStateOf(t, out)
	assert.Equal(t, "block-node", bn.ReleaseInfo.Name)
	require.NotNil(t, bn.Shaping)
	assert.Equal(t, "eth0", bn.Shaping.EgressInterface)
	assert.Equal(t, "1gbit", bn.Shaping.LinkRate)
}

func TestTrafficShapingEnabledV1_Applies_NoFile(t *testing.T) {
	m := &TrafficShapingEnabledV1Migration{stateFileOverride: filepath.Join(t.TempDir(), "state.yaml")}
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies)
}

func TestTrafficShapingEnabledV1_ExecuteThenRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte(shapingOnLegacyYAML), 0o600))
	m := &TrafficShapingEnabledV1Migration{stateFileOverride: path}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.True(t, applies)

	require.NoError(t, m.Execute(context.Background(), nil))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, blockNodeStateOf(t, b).TrafficShapingEnabled)

	applies, err = m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies, "must not re-apply after Execute")

	require.NoError(t, m.Rollback(context.Background(), nil))
	b, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), trafficShapingEnabledKey)

	applies, err = m.Applies(nil)
	require.NoError(t, err)
	assert.True(t, applies, "must apply again after Rollback")
}
