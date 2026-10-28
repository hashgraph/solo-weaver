// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConsensusOrbitStateFromSpec(t *testing.T) {
	spec := map[string]interface{}{
		"provisionerDaemonEnabled": true,
		"requireDigestOnDeploy":    true,
		"deploymentModel":          "SingleKubernetesCluster",
		"consensus": map[string]interface{}{
			"provisioningModel": "InitContainerJava",
			"genesis": map[string]interface{}{"addressBook": map[string]interface{}{
				"ledgerId": "0x00", "chainId": "295",
				"realmId": int64(1), "shardId": float64(2),
				"externalAddresses": []interface{}{
					map[string]interface{}{"nodeId": int64(9), "nickName": "ext"},
				},
			}},
		},
	}
	got := ConsensusOrbitStateFromSpec(spec)
	assert.True(t, got.ProvisionerDaemonEnabled)
	assert.True(t, got.RequireDigestOnDeploy)
	assert.Equal(t, "SingleKubernetesCluster", got.DeploymentModel)
	assert.Equal(t, "InitContainerJava", got.ProvisioningModel)
	assert.Equal(t, "0x00", got.LedgerId)
	assert.Equal(t, "295", got.ChainId)
	assert.Equal(t, 1, got.RealmId)
	assert.Equal(t, 2, got.ShardId)
	require.Len(t, got.ExternalAddresses, 1)
	assert.Equal(t, int64(9), got.ExternalAddresses[0].NodeId)
	assert.NotEmpty(t, got.ExternalAddresses[0].Digest)

	assert.Equal(t, ConsensusOrbitState{}, ConsensusOrbitStateFromSpec(nil), "an empty spec is all zero values")
}

func TestConsensusOrbitState_Diff(t *testing.T) {
	base := ConsensusOrbitState{LedgerId: "0x00", ChainId: "295"}

	t.Run("equal", func(t *testing.T) { assert.Empty(t, base.Diff(base)) })

	t.Run("empty chainId equals 0", func(t *testing.T) {
		assert.Empty(t, ConsensusOrbitState{LedgerId: "0x00"}.Diff(ConsensusOrbitState{LedgerId: "0x00", ChainId: "0"}))
	})

	t.Run("external addresses are matched by node id and digest", func(t *testing.T) {
		want := ConsensusOrbitState{ExternalAddresses: []OrbitExternalAddress{{NodeId: 1, Digest: "a"}, {NodeId: 2, Digest: "b"}}}
		live := ConsensusOrbitState{ExternalAddresses: []OrbitExternalAddress{{NodeId: 2, Digest: "x"}, {NodeId: 3, Digest: "c"}}}
		diffs := want.Diff(live)
		require.Len(t, diffs, 3)
		assert.Equal(t, "externalAddresses[1]", diffs[0].Field)
		assert.Equal(t, "absent", diffs[0].Live)
		assert.Equal(t, "externalAddresses[2]", diffs[1].Field)
		assert.Equal(t, "externalAddresses[3]", diffs[2].Field)
		assert.Equal(t, "present", diffs[2].Live)
	})
}

func TestConsensusOrbitState_YAMLRoundTrip(t *testing.T) {
	in := ConsensusOrbitState{
		ProvisionerDaemonEnabled: true, LedgerId: "0x00", ChainId: "295", RealmId: 1,
		ExternalAddresses: []OrbitExternalAddress{{NodeId: 4, Digest: "d"}},
	}
	b, err := yaml.Marshal(in)
	require.NoError(t, err)
	var out ConsensusOrbitState
	require.NoError(t, yaml.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}
