// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestImageSource_VersionTag(t *testing.T) {
	// A deterministic manifest shares one version across every registry, so the
	// shared tag identifies the source's version.
	src := &ImageSource{
		Repositories: []ImageRepositoryRef{
			{Repository: "gcr.io/hedera-registry", ImageName: "consensus-node", ImageTag: "0.74.2"},
			{Repository: "docker.io/hashgraph", ImageName: "consensus-node", ImageTag: "0.74.2"},
		},
		LayerHashes: map[string][]string{"linux/amd64": {"sha256:aaa"}},
	}
	assert.Equal(t, "0.74.2", src.VersionTag())

	// A nil or empty source has no version.
	assert.Equal(t, "", (*ImageSource)(nil).VersionTag())
	assert.Equal(t, "", (&ImageSource{}).VersionTag())

	// Candidates that disagree on the tag are not a single version, so the guard
	// in the install handler must not treat them as matching any effective tag.
	mixed := &ImageSource{
		Repositories: []ImageRepositoryRef{
			{Repository: "gcr.io/hedera-registry", ImageName: "consensus-node", ImageTag: "0.74.2"},
			{Repository: "docker.io/hashgraph", ImageName: "consensus-node", ImageTag: "0.75.0"},
		},
	}
	assert.Equal(t, "", mixed.VersionTag())
}
