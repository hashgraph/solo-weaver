// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func testNode(name, cidr string) unstructured.Unstructured {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{"name": name},
	}
	if cidr != "" {
		obj["spec"] = map[string]interface{}{"podCIDR": cidr}
	}
	return unstructured.Unstructured{Object: obj}
}

func TestSelectNodePodCIDR(t *testing.T) {
	t.Run("matches by hostname", func(t *testing.T) {
		nodes := []unstructured.Unstructured{
			testNode("node-a", "10.4.0.0/24"),
			testNode("node-b", "10.4.1.0/24"),
		}
		cidr, err := selectNodePodCIDR(nodes, "node-b")
		require.NoError(t, err)
		require.Equal(t, "10.4.1.0/24", cidr)
	})

	t.Run("single-node fallback when hostname does not match", func(t *testing.T) {
		nodes := []unstructured.Unstructured{testNode("some-other-name", "10.4.0.0/24")}
		cidr, err := selectNodePodCIDR(nodes, "unrelated-host")
		require.NoError(t, err)
		require.Equal(t, "10.4.0.0/24", cidr)
	})

	t.Run("empty hostname falls back on single node", func(t *testing.T) {
		nodes := []unstructured.Unstructured{testNode("only", "10.4.2.0/24")}
		cidr, err := selectNodePodCIDR(nodes, "")
		require.NoError(t, err)
		require.Equal(t, "10.4.2.0/24", cidr)
	})

	t.Run("no nodes is an error", func(t *testing.T) {
		_, err := selectNodePodCIDR(nil, "node-a")
		require.Error(t, err)
	})

	t.Run("ambiguous multi-node without hostname match is an error", func(t *testing.T) {
		nodes := []unstructured.Unstructured{
			testNode("node-a", "10.4.0.0/24"),
			testNode("node-b", "10.4.1.0/24"),
		}
		_, err := selectNodePodCIDR(nodes, "node-c")
		require.Error(t, err)
	})

	t.Run("node without podCIDR is an error", func(t *testing.T) {
		nodes := []unstructured.Unstructured{testNode("node-a", "")}
		_, err := selectNodePodCIDR(nodes, "node-a")
		require.Error(t, err)
	})
}
