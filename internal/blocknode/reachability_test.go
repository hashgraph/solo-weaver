// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"
)

func TestBlockNodePublicPort_IsWellKnownValue(t *testing.T) {
	t.Parallel()
	const want int64 = 40840
	if BlockNodePublicPort != want {
		t.Fatalf("BlockNodePublicPort = %d, want %d — this is the ecosystem-wide BN gRPC port; changing it requires coordination with every downstream SDK and tool", BlockNodePublicPort, want)
	}
}
