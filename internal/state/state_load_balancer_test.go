// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLoadBalancerEnabled_Persistence(t *testing.T) {
	t.Run("a file written before the field existed loads it as unknown", func(t *testing.T) {
		old := []byte(`
state:
  version: v2
  blockNodeState:
    name: block-node
    version: 0.40.0
`)
		var got State
		require.NoError(t, yaml.Unmarshal(old, &got))
		require.Nil(t, got.BlockNodeState.LoadBalancerEnabled)
	})

	for _, enabled := range []bool{true, false} {
		t.Run("a recorded "+strconv.FormatBool(enabled)+" survives a write and a load", func(t *testing.T) {
			original := State{StateRecord: StateRecord{BlockNodeState: BlockNodeState{LoadBalancerEnabled: &enabled}}}

			out, err := yaml.Marshal(original)
			require.NoError(t, err)
			require.Contains(t, string(out), "loadBalancerEnabled: "+strconv.FormatBool(enabled))

			var got State
			require.NoError(t, yaml.Unmarshal(out, &got))
			require.NotNil(t, got.BlockNodeState.LoadBalancerEnabled)
			require.Equal(t, enabled, *got.BlockNodeState.LoadBalancerEnabled)
		})
	}
}

// Refresh decodes the state file onto a clone, and yaml.v3 writes through a
// non-nil pointer, so the clone must not share the original's.
func TestLoadBalancerEnabled_CloneDoesNotShareTheChoice(t *testing.T) {
	enabled := true
	original := &State{StateRecord: StateRecord{BlockNodeState: BlockNodeState{LoadBalancerEnabled: &enabled}}}

	clone, err := original.Clone()
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal([]byte("state:\n  blockNodeState:\n    loadBalancerEnabled: false\n"), clone))

	require.False(t, *clone.BlockNodeState.LoadBalancerEnabled)
	require.True(t, *original.BlockNodeState.LoadBalancerEnabled)
}
