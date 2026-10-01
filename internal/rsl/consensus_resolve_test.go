// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/manifests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildImageSource_DeterministicMultiRegistry(t *testing.T) {
	img := &manifests.Image{
		Version:           "0.74.2",
		SelectionStrategy: "Sequential",
		Registries: []manifests.Registry{
			{Image: "gcr.io/hedera-registry/consensus-node:0.74.2"},
			{Image: "docker.io/hashgraph/consensus-node:0.74.2"},
		},
		Deterministic: &manifests.Deterministic{
			Supported:   true,
			LayerHashes: manifests.LayerHashes{"linux/amd64": {"sha256:aaa"}},
		},
	}

	src := buildImageSource(img)
	require.NotNil(t, src)
	require.Len(t, src.Repositories, 2)
	assert.Equal(t, "gcr.io/hedera-registry", src.Repositories[0].Repository)
	assert.Equal(t, "consensus-node", src.Repositories[0].ImageName)
	assert.Equal(t, "0.74.2", src.Repositories[0].ImageTag)
	assert.Equal(t, "docker.io/hashgraph", src.Repositories[1].Repository)
	assert.Equal(t, []string{"sha256:aaa"}, src.LayerHashes["linux/amd64"])
	// Manifest selection strategy must be carried through so the capsule step
	// can apply it when there's no CLI override.
	assert.Equal(t, "Sequential", src.SelectionStrategy)
}

func TestBuildImageSource_NotRepresentable(t *testing.T) {
	det := &manifests.Deterministic{Supported: true, LayerHashes: manifests.LayerHashes{"linux/amd64": {"sha256:x"}}}

	assert.Nil(t, buildImageSource(nil))

	// Single registry ⇒ nil.
	assert.Nil(t, buildImageSource(&manifests.Image{
		Version:       "1",
		Registries:    []manifests.Registry{{Image: "r/n:1"}},
		Deterministic: det,
	}))

	// Non-deterministic (per-registry hashes) ⇒ nil.
	assert.Nil(t, buildImageSource(&manifests.Image{
		Version: "1",
		Registries: []manifests.Registry{
			{Image: "a/n:1", LayerHashes: manifests.LayerHashes{"linux/amd64": {"sha256:a"}}},
			{Image: "b/n:1", LayerHashes: manifests.LayerHashes{"linux/amd64": {"sha256:b"}}},
		},
	}))

	// Multi-registry but no shared hashes ⇒ nil.
	assert.Nil(t, buildImageSource(&manifests.Image{
		Version:    "1",
		Registries: []manifests.Registry{{Image: "a/n:1"}, {Image: "b/n:1"}},
	}))
}

func TestStripImageTag(t *testing.T) {
	assert.Equal(t, "gcr.io/r/n", stripImageTag("gcr.io/r/n:1.2.3"))
	assert.Equal(t, "gcr.io/r/n", stripImageTag("gcr.io/r/n"))
	// Registry port is not a tag.
	assert.Equal(t, "host:5000/r/n", stripImageTag("host:5000/r/n"))
	assert.Equal(t, "host:5000/r/n", stripImageTag("host:5000/r/n:1"))
}
