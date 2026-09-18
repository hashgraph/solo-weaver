// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoragePathsByVolume_BasePathDerivesEveryLocalVolume(t *testing.T) {
	paths, err := StoragePathsByVolume(models.BlockNodeStorage{BasePath: "/opt/hedera/blocknode"}, "0.37.0")
	require.NoError(t, err)

	// Exactly the weaver-managed local volumes and nothing else: the RFH
	// cloud-storage-archive bucket has no path here, so the media check can
	// never flag it.
	assert.Equal(t, map[string]string{
		VolumeArchive:       "/opt/hedera/blocknode/archive",
		VolumeLive:          "/opt/hedera/blocknode/live",
		VolumeLog:           "/opt/hedera/blocknode/logs",
		"plugins":           "/opt/hedera/blocknode/plugins",
		"application-state": "/opt/hedera/blocknode/application-state",
	}, paths)
}

func TestStoragePathsByVolume_OptionalVolumesFollowTheChartVersion(t *testing.T) {
	// 0.30.0 still has verification and has no application-state yet.
	paths, err := StoragePathsByVolume(models.BlockNodeStorage{BasePath: "/data"}, "0.30.0")
	require.NoError(t, err)

	assert.Equal(t, "/data/verification", paths["verification"])
	assert.Equal(t, "/data/plugins", paths["plugins"])
	assert.NotContains(t, paths, "application-state")
}

func TestStoragePathsByVolume_IndividualPathsWinOverBasePath(t *testing.T) {
	paths, err := StoragePathsByVolume(models.BlockNodeStorage{
		BasePath:    "/data",
		ArchivePath: "/mnt/archive",
		PluginsPath: "/mnt/plugins",
	}, "0.37.0")
	require.NoError(t, err)

	assert.Equal(t, "/mnt/archive", paths[VolumeArchive])
	assert.Equal(t, "/data/live", paths[VolumeLive])
	assert.Equal(t, "/mnt/plugins", paths["plugins"])
}

func TestStoragePathsByVolume_IncompleteStorageIsAnError(t *testing.T) {
	paths, err := StoragePathsByVolume(models.BlockNodeStorage{ArchivePath: "/mnt/archive"}, "0.37.0")
	require.Error(t, err, "no base path and missing individual paths cannot be resolved")
	assert.Nil(t, paths)
}
