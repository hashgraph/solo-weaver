// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveShapeToObserved(t *testing.T) {
	live := liveConsensusShape{
		ContainerName:               "root",
		CPULimit:                    "4",
		CPURequest:                  "2",
		MemoryLimit:                 "16Gi",
		MemoryRequest:               "8Gi",
		JavaHeapMin:                 "8g",
		JavaHeapMax:                 "12g",
		JavaOpts:                    "-XX:+UseZGC",
		UCImageRepo:                 "ghcr.io/hiero/solo-operator",
		UCImageTag:                  "0.8.0",
		ProvisionerDaemonEnabled:    true,
		ProvisionerDaemonEnabledSet: true,
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
	assert.True(t, obs.ProvisionerDaemonEnabled)
	assert.True(t, obs.ProvisionerDaemonEnabledSet)
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

func TestReadProvisionerDaemonEnabled(t *testing.T) {
	tests := []struct {
		name    string
		spec    map[string]interface{}
		found   bool
		wantSet bool
		want    bool
	}{
		{"true", map[string]interface{}{"provisionerDaemonEnabled": true}, true, true, true},
		{"explicit false", map[string]interface{}{"provisionerDaemonEnabled": false}, true, true, false},
		{"omitted key on a readable Orbit means false", map[string]interface{}{"other": 1}, true, true, false},
		{"non-bool value is not trusted", map[string]interface{}{"provisionerDaemonEnabled": "yes"}, true, false, false},
		{"orbit spec not found", nil, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kc := orbitSpecKubeClient{spec: tc.spec, found: tc.found}
			var live liveConsensusShape
			readProvisionerDaemonEnabled(context.Background(), kc, "v1", "orbit", &live)
			assert.Equal(t, tc.wantSet, live.ProvisionerDaemonEnabledSet)
			assert.Equal(t, tc.want, live.ProvisionerDaemonEnabled)
		})
	}
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
