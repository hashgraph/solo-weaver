// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
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
