// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A node without its kubelet or runtime version is a failed read, so the
// recorded values are kept rather than saved as empty.
func TestNodeInfoField(t *testing.T) {
	node := func(nodeInfo any) map[string]any {
		return map[string]any{"status": map[string]any{"nodeInfo": nodeInfo}}
	}

	v, err := nodeInfoField(node(map[string]any{"kubeletVersion": "v1.36.5"}), "kubeletVersion")
	require.NoError(t, err)
	require.Equal(t, "v1.36.5", v)

	for name, obj := range map[string]map[string]any{
		"missing":      node(map[string]any{}),
		"empty":        node(map[string]any{"kubeletVersion": ""}),
		"not a string": node(map[string]any{"kubeletVersion": int64(1)}),
		"no status":    {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := nodeInfoField(obj, "kubeletVersion")
			require.Error(t, err)
		})
	}
}
