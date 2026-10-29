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

// shapingOnLegacyYAML is a state.yaml for a node with shaping on under the
// opt-out encoding: no decision key, a shaping record.
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

// rawBlockNodeState decodes blockNodeState as a plain map so tests can tell a
// missing key from a false one.
func rawBlockNodeState(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var doc struct {
		State struct {
			BlockNodeState map[string]any `yaml:"blockNodeState"`
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

func TestMigrateToTrafficShapingEnabled(t *testing.T) {
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
			name: "opt-out with a kept shaping record is disabled",
			in: `
state:
    blockNodeState:
        trafficShapingDisabled: true
        shaping: {}
`,
			wantApplies: true,
		},
		{
			name: "opt-out without shaping is disabled",
			in: `
state:
    blockNodeState:
        trafficShapingDisabled: true
`,
			wantApplies: true,
		},
		{
			name: "no record is disabled",
			in: `
state:
    blockNodeState:
        name: block-node
`,
			wantApplies: true,
		},
		{
			name: "null shaping is disabled",
			in: `
state:
    blockNodeState:
        shaping: null
`,
			wantApplies: true,
		},
		{
			name: "new key wins over a leftover opt-out",
			in: `
state:
    blockNodeState:
        trafficShapingEnabled: true
        trafficShapingDisabled: true
`,
			wantApplies: true,
			wantEnabled: true,
		},
		{
			name: "already migrated, enabled",
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
			name: "already migrated, disabled with a kept shaping record",
			in: `
state:
    blockNodeState:
        trafficShapingEnabled: false
        shaping: {}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantApplies, needsTrafficShapingEnabled([]byte(tc.in)))

			out, err := migrateToTrafficShapingEnabled([]byte(tc.in))
			require.NoError(t, err)

			bn := rawBlockNodeState(t, out)
			assert.Equal(t, tc.wantEnabled, bn[trafficShapingEnabledKey])
			assert.NotContains(t, bn, trafficShapingDisabledKey)
			assert.False(t, needsTrafficShapingEnabled(out), "must not apply twice")
		})
	}
}

func TestMigrateToTrafficShapingEnabled_NoBlockNodeState(t *testing.T) {
	in := []byte("state:\n    version: v2\n")
	assert.False(t, needsTrafficShapingEnabled(in))

	out, err := migrateToTrafficShapingEnabled(in)
	require.NoError(t, err)
	assert.NotContains(t, string(out), trafficShapingEnabledKey)
}

func TestMigrateToTrafficShapingEnabled_KeepsOtherFields(t *testing.T) {
	out, err := migrateToTrafficShapingEnabled([]byte(shapingOnLegacyYAML))
	require.NoError(t, err)

	var doc struct {
		State struct {
			BlockNodeState BlockNodeState `yaml:"blockNodeState"`
		} `yaml:"state"`
	}
	require.NoError(t, yaml.Unmarshal(out, &doc))
	bn := doc.State.BlockNodeState
	assert.Equal(t, "block-node", bn.ReleaseInfo.Name)
	require.NotNil(t, bn.Shaping)
	assert.Equal(t, "eth0", bn.Shaping.EgressInterface)
	assert.Equal(t, "1gbit", bn.Shaping.LinkRate)
}

func TestMigrateToTrafficShapingDisabled(t *testing.T) {
	for _, tc := range []struct {
		name         string
		in           string
		wantDisabled bool
	}{
		{name: "enabled drops both keys", in: "state:\n    blockNodeState:\n        trafficShapingEnabled: true\n"},
		{name: "disabled writes the opt-out", in: "state:\n    blockNodeState:\n        trafficShapingEnabled: false\n", wantDisabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := migrateToTrafficShapingDisabled([]byte(tc.in))
			require.NoError(t, err)

			bn := rawBlockNodeState(t, out)
			assert.NotContains(t, bn, trafficShapingEnabledKey)
			if tc.wantDisabled {
				assert.Equal(t, true, bn[trafficShapingDisabledKey])
			} else {
				assert.NotContains(t, bn, trafficShapingDisabledKey)
			}
		})
	}
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
	assert.Equal(t, true, rawBlockNodeState(t, b)[trafficShapingEnabledKey])

	applies, err = m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies, "must not re-apply after Execute")

	require.NoError(t, m.Rollback(context.Background(), nil))
	b, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, rawBlockNodeState(t, b), trafficShapingEnabledKey)

	applies, err = m.Applies(nil)
	require.NoError(t, err)
	assert.True(t, applies, "must apply again after Rollback")
}

// TestBlockNodeState_WritesTrafficShapingEnabledWhenFalse pins the missing
// omitempty: if false were omitted, a disabled node with a kept shaping record
// would look like a legacy enabled node and the migration would turn it on.
func TestBlockNodeState_WritesTrafficShapingEnabledWhenFalse(t *testing.T) {
	out, err := yaml.Marshal(BlockNodeState{Shaping: &ShapingState{}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "trafficShapingEnabled: false")
}
