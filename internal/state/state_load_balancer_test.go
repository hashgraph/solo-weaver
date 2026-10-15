// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"strconv"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestServiceExposure_Persistence(t *testing.T) {
	t.Run("a file written before the fields existed loads them as unknown", func(t *testing.T) {
		old := []byte(`
state:
  version: v2
  blockNodeState:
    name: block-node
    version: 0.40.0
`)
		var got State
		require.NoError(t, yaml.Unmarshal(old, &got))
		require.Empty(t, got.BlockNodeState.ServiceTopology)
		require.Nil(t, got.BlockNodeState.MetalLBPool)
	})

	for _, pool := range []bool{true, false} {
		t.Run("a recorded pool of "+strconv.FormatBool(pool)+" survives a write and a load", func(t *testing.T) {
			original := State{StateRecord: StateRecord{BlockNodeState: BlockNodeState{
				ServiceTopology: ServiceTopologySplit,
				MetalLBPool:     &pool,
			}}}

			out, err := yaml.Marshal(original)
			require.NoError(t, err)
			require.Contains(t, string(out), "serviceTopology: split")
			require.Contains(t, string(out), "metallbPool: "+strconv.FormatBool(pool))

			var got State
			require.NoError(t, yaml.Unmarshal(out, &got))
			require.Equal(t, ServiceTopologySplit, got.BlockNodeState.ServiceTopology)
			require.NotNil(t, got.BlockNodeState.MetalLBPool)
			require.Equal(t, pool, *got.BlockNodeState.MetalLBPool)
		})
	}
}

// yaml.v3 writes through a non-nil pointer and merges into an existing map, so
// decoding onto a clone must leave the original untouched.
func TestBlockNodeState_CloneSharesNothingADecodeCanChange(t *testing.T) {
	pool, prio := true, 1
	original := &State{StateRecord: StateRecord{BlockNodeState: BlockNodeState{
		MetalLBPool: &pool,
		Shaping: &ShapingState{
			LinkRate:       "1gbit",
			ShapeOverrides: map[string]models.ShapeOverride{"publisher": {Rate: "800mbit", Prio: &prio}},
		},
	}}}

	clone, err := original.Clone()
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal([]byte(`
state:
  blockNodeState:
    metallbPool: false
    shaping:
      linkRate: 10gbit
      shapeOverrides:
        publisher: {rate: 500mbit, prio: 2}
        partner: {rate: 400mbit}
`), clone))

	bn := original.BlockNodeState
	require.True(t, *bn.MetalLBPool)
	require.Equal(t, "1gbit", bn.Shaping.LinkRate)
	require.Equal(t, 1, *bn.Shaping.ShapeOverrides["publisher"].Prio)
	require.NotContains(t, bn.Shaping.ShapeOverrides, "partner")
}
