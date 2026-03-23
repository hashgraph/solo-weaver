// SPDX-License-Identifier: Apache-2.0

package labels

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderLabelRules(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected []string
	}{
		{
			name: "multiple labels sorted alphabetically",
			labels: map[string]string{
				"cluster":       "lfh02-previewnet-blocknode",
				"environment":   "previewnet",
				"instance_type": "lfh",
				"ip":            "10.0.0.1",
			},
			expected: []string{
				`target_label = "cluster"`,
				`replacement  = "lfh02-previewnet-blocknode"`,
				`target_label = "environment"`,
				`replacement  = "previewnet"`,
				`target_label = "instance_type"`,
				`replacement  = "lfh"`,
				`target_label = "ip"`,
				`replacement  = "10.0.0.1"`,
			},
		},
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: nil,
		},
		{
			name: "only cluster label is rendered",
			labels: map[string]string{
				"cluster": "test",
			},
			expected: []string{
				`target_label = "cluster"`,
				`replacement  = "test"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderLabelRules(tt.labels)
			if tt.expected == nil {
				assert.Empty(t, result)
			} else {
				for _, exp := range tt.expected {
					assert.Contains(t, result, exp)
				}
			}
		})
	}
}

func TestRenderLabelRules_Ordering(t *testing.T) {
	labels := map[string]string{
		"cluster":       "test",
		"zebra":         "z",
		"alpha":         "a",
		"instance_type": "bn",
	}
	result := RenderLabelRules(labels)

	clusterPos := strings.Index(result, `"cluster"`)
	alphaPos := strings.Index(result, `"alpha"`)
	instancePos := strings.Index(result, `"instance_type"`)
	zebraPos := strings.Index(result, `"zebra"`)

	require.Greater(t, clusterPos, -1)
	require.Greater(t, alphaPos, -1)
	require.Greater(t, instancePos, -1)
	require.Greater(t, zebraPos, -1)

	assert.Less(t, alphaPos, clusterPos, "alpha should come before cluster")
	assert.Less(t, clusterPos, instancePos, "cluster should come before instance_type")
	assert.Less(t, instancePos, zebraPos, "instance_type should come before zebra")
}

func TestRenderStaticLabels(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected []string
	}{
		{
			name: "multiple labels sorted alphabetically",
			labels: map[string]string{
				"cluster":     "test-cluster",
				"environment": "previewnet",
				"ip":          "10.0.0.1",
			},
			expected: []string{
				`cluster = "test-cluster",`,
				`environment = "previewnet",`,
				`ip = "10.0.0.1",`,
			},
		},
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderStaticLabels(tt.labels)
			if tt.expected == nil {
				assert.Empty(t, result)
			} else {
				for _, exp := range tt.expected {
					assert.Contains(t, result, exp)
				}
			}
		})
	}
}
