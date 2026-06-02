// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"
)

func TestDiffServiceSpecs(t *testing.T) {
	t.Parallel()

	clusterIPSpec := map[string]interface{}{
		"type": "ClusterIP",
		"ports": []interface{}{
			map[string]interface{}{"name": "port-block-node", "port": int64(40840)},
		},
	}
	loadBalancerSpec := map[string]interface{}{
		"type":           "LoadBalancer",
		"loadBalancerIP": "192.168.68.200",
		"ports": []interface{}{
			map[string]interface{}{"name": "port-block-node", "port": int64(40840)},
		},
	}
	clusterIPSpecCopy := map[string]interface{}{
		"type": "ClusterIP",
		"ports": []interface{}{
			map[string]interface{}{"name": "port-block-node", "port": int64(40840)},
		},
	}

	tests := []struct {
		name        string
		before      map[string]map[string]interface{}
		after       map[string]map[string]interface{}
		wantChanged bool
	}{
		{
			name:        "both empty → unchanged",
			before:      map[string]map[string]interface{}{},
			after:       map[string]map[string]interface{}{},
			wantChanged: false,
		},
		{
			name: "identical single-service snapshot → unchanged",
			before: map[string]map[string]interface{}{
				"bn":          clusterIPSpec,
				"bn-external": loadBalancerSpec,
			},
			after: map[string]map[string]interface{}{
				"bn":          clusterIPSpecCopy,
				"bn-external": loadBalancerSpec,
			},
			wantChanged: false,
		},
		{
			name: "Service removed → changed",
			before: map[string]map[string]interface{}{
				"bn":          clusterIPSpec,
				"bn-external": loadBalancerSpec,
			},
			after: map[string]map[string]interface{}{
				"bn": loadBalancerSpec, // Shape A → Shape B flip: -external is gone
			},
			wantChanged: true,
		},
		{
			name: "Service added → changed",
			before: map[string]map[string]interface{}{
				"bn": loadBalancerSpec,
			},
			after: map[string]map[string]interface{}{
				"bn":          clusterIPSpec,
				"bn-external": loadBalancerSpec,
			},
			wantChanged: true,
		},
		{
			name: "spec.type flipped → changed",
			before: map[string]map[string]interface{}{
				"bn": clusterIPSpec,
			},
			after: map[string]map[string]interface{}{
				"bn": loadBalancerSpec,
			},
			wantChanged: true,
		},
		{
			name: "same names, different counts (defensive) → changed",
			before: map[string]map[string]interface{}{
				"bn": clusterIPSpec,
			},
			after: map[string]map[string]interface{}{
				"bn":          clusterIPSpec,
				"bn-external": loadBalancerSpec,
			},
			wantChanged: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := diffServiceSpecs(tc.before, tc.after)
			if got != tc.wantChanged {
				t.Fatalf("diffServiceSpecs = %v, want %v", got, tc.wantChanged)
			}
		})
	}
}

func TestBlockNodePublicPort_IsWellKnownValue(t *testing.T) {
	t.Parallel()
	const want int64 = 40840
	if BlockNodePublicPort != want {
		t.Fatalf("BlockNodePublicPort = %d, want %d — this is the ecosystem-wide BN gRPC port; changing it requires coordination with every downstream SDK and tool", BlockNodePublicPort, want)
	}
}
