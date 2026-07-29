// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestLoadBalancerPort_UsesServiceAdvertisedPort(t *testing.T) {
	lb := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"ports": []interface{}{
				map[string]interface{}{"port": int64(50051)},
			},
		},
	}}
	require.Equal(t, int64(50051), loadBalancerPort(lb),
		"the probe targets the port the Service actually announces")
}

func TestLoadBalancerPort_FallsBackWhenNoPorts(t *testing.T) {
	lb := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{},
	}}
	require.Equal(t, BlockNodePublicPort, loadBalancerPort(lb),
		"a Service with no ports falls back to the well-known public port")
}
