// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveShapeToObserved(t *testing.T) {
	live := liveConsensusShape{
		ContainerName: "root",
		CPULimit:      "4",
		CPURequest:    "2",
		MemoryLimit:   "16Gi",
		MemoryRequest: "8Gi",
		JavaHeapMin:   "8g",
		JavaHeapMax:   "12g",
		JavaOpts:      "-XX:+UseZGC",
		UCImageRepo:   "ghcr.io/hiero/solo-operator",
		UCImageTag:    "0.8.0",
		Orbit:         &state.ConsensusOrbitState{ProvisionerDaemonEnabled: true},
		Volumes: models.ConsensusVolumeConfig{
			Volumes: map[string]models.ConsensusVolumeSpec{
				"saved": {Type: "hostpath", Path: "/data/saved"},
			},
		},
		VolumesSet: true,
		ImagePullSecrets: models.PullSecretSelector{
			ByHost: map[string]string{"gcr.io": "gcr-creds"},
		},
		ImagePullSecretsSet: true,
	}

	obs := liveShapeToObserved(live)
	require.NotNil(t, obs)

	assert.Equal(t, "root", obs.ContainerName)
	assert.Equal(t, "4", obs.CPULimit)
	assert.Equal(t, "16Gi", obs.MemoryLimit)
	assert.Equal(t, "-XX:+UseZGC", obs.JavaOpts)
	assert.Equal(t, "ghcr.io/hiero/solo-operator", obs.UCImageRepo)
	require.NotNil(t, obs.Orbit)
	assert.True(t, obs.Orbit.ProvisionerDaemonEnabled)
	assert.True(t, obs.VolumesSet)
	assert.Equal(t, "hostpath", string(obs.Volumes.Volumes["saved"].Type))
	assert.True(t, obs.ImagePullSecretsSet)
	assert.Equal(t, "gcr-creds", obs.ImagePullSecrets.ByHost["gcr.io"])
}

func TestReadHostPathOwners(t *testing.T) {
	dir := t.TempDir()
	live := liveConsensusShape{Volumes: models.ConsensusVolumeConfig{Volumes: map[string]models.ConsensusVolumeSpec{
		"saved":  {Type: models.VolumeBackingHostPath, Path: dir},
		"gone":   {Type: models.VolumeBackingHostPath, Path: dir + "/missing"},
		"events": {Type: models.VolumeBackingEmptyDir},
	}}}

	readHostPathOwners(&live)

	require.Contains(t, live.HostPathOwners, "saved")
	assert.Equal(t, os.Getuid(), live.HostPathOwners["saved"].UID)
	assert.NotContains(t, live.HostPathOwners, "gone")
	assert.NotContains(t, live.HostPathOwners, "events")
}

// containersKubeClient serves only the capsule's spec.podProperties.containers map.
type containersKubeClient struct {
	ConsensusKubeClient
	containers map[string]interface{}
	err        error
}

func (c containersKubeClient) GetResourceNestedMap(
	_ context.Context, _, _, _, _ string, fields ...string,
) (map[string]interface{}, bool, error) {
	if c.err != nil {
		return nil, false, c.err
	}
	if len(fields) == 3 && fields[2] == fieldContainers {
		return c.containers, c.containers != nil, nil
	}
	return nil, false, nil
}

func TestReadLiveConsensusShape(t *testing.T) {
	cn := map[string]interface{}{
		"name":     "root",
		"javaOpts": "-XX:+UseZGC",
		// javaHeapMin deliberately absent: a deleted field.
		"resources": map[string]interface{}{
			"limits": map[string]interface{}{"cpu": "4", "memory": "16Gi"},
		},
	}
	c := &consensusChecker{}

	t.Run("missing field reads as absent, not skipped", func(t *testing.T) {
		kc := containersKubeClient{containers: map[string]interface{}{"consensusNode": cn}}
		live, ok := c.readLiveConsensusShape(context.Background(), kc, "v1", "ns", "capsule")
		require.True(t, ok)
		assert.Equal(t, "root", live.ContainerName)
		assert.Equal(t, "-XX:+UseZGC", live.JavaOpts)
		assert.Equal(t, "4", live.CPULimit)
		assert.Equal(t, "", live.JavaHeapMin)
		assert.Equal(t, "", live.MemoryRequest)
	})

	t.Run("no containers at all still reads (everything absent)", func(t *testing.T) {
		live, ok := c.readLiveConsensusShape(context.Background(), containersKubeClient{}, "v1", "ns", "capsule")
		require.True(t, ok)
		assert.Equal(t, "", live.ContainerName)
	})

	t.Run("read error skips the whole shape", func(t *testing.T) {
		kc := containersKubeClient{err: errors.New("boom")}
		_, ok := c.readLiveConsensusShape(context.Background(), kc, "v1", "ns", "capsule")
		assert.False(t, ok)
	})
}

func TestReadLiveOrbit(t *testing.T) {
	t.Run("reads the tracked fields; an omitted daemon key is false", func(t *testing.T) {
		kc := orbitSpecKubeClient{found: true, spec: map[string]interface{}{
			"requireDigestOnDeploy": true,
			"consensus": map[string]interface{}{
				"genesis": map[string]interface{}{"addressBook": map[string]interface{}{
					"ledgerId": "0x01", "chainId": "296", "realmId": int64(1),
				}},
			},
		}}
		var live liveConsensusShape
		readLiveOrbit(context.Background(), kc, "v1", "orbit", &live)
		require.NotNil(t, live.Orbit)
		assert.False(t, live.Orbit.ProvisionerDaemonEnabled)
		assert.True(t, live.Orbit.RequireDigestOnDeploy)
		assert.Equal(t, "0x01", live.Orbit.LedgerId)
		assert.Equal(t, "296", live.Orbit.ChainId)
		assert.Equal(t, 1, live.Orbit.RealmId)
	})

	t.Run("an unreadable Orbit leaves it nil", func(t *testing.T) {
		var live liveConsensusShape
		readLiveOrbit(context.Background(), orbitSpecKubeClient{found: false}, "v1", "orbit", &live)
		assert.Nil(t, live.Orbit)
	})
}

// orbitSpecKubeClient serves only an Orbit's spec map.
type orbitSpecKubeClient struct {
	ConsensusKubeClient
	spec  map[string]interface{}
	found bool
}

func (c orbitSpecKubeClient) GetResourceNestedMap(
	context.Context, string, string, string, string, ...string,
) (map[string]interface{}, bool, error) {
	return c.spec, c.found, nil
}
