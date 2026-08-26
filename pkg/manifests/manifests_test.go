// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSupportedVersions(t *testing.T) {
	t.Run("returns v1 for every known kind", func(t *testing.T) {
		for _, kind := range []Kind{
			KindConsensusNodeComponents,
			KindInfrastructureVersions,
			KindExternalFiles,
			KindStateSources,
		} {
			got := SupportedVersions(kind)
			require.Equal(t, []SchemaVersion{SchemaV1}, got, "kind=%s", kind)
		}
	})

	t.Run("returns nil for unknown kind", func(t *testing.T) {
		require.Nil(t, SupportedVersions(Kind("nope")))
	})
}
