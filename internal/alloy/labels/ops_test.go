// SPDX-License-Identifier: Apache-2.0

package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseClusterName(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expected    map[string]string
	}{
		{
			name:        "full convention: instance-environment-suffix",
			clusterName: "lfh02-previewnet-blocknode",
			expected: map[string]string{
				"instance_type": "lfh",
			},
		},
		{
			name:        "two segments only",
			clusterName: "node01-mainnet",
			expected: map[string]string{
				"instance_type": "node",
			},
		},
		{
			name:        "single segment",
			clusterName: "mycluster",
			expected: map[string]string{
				"instance_type": "mycluster",
			},
		},
		{
			name:        "numeric-only first segment",
			clusterName: "123-testnet-blocknode",
			expected:    map[string]string{},
		},
		{
			name:        "empty cluster name",
			clusterName: "",
			expected:    map[string]string{},
		},
		{
			name:        "multiple dashes in suffix",
			clusterName: "bn01-perfnet-block-node-extra",
			expected: map[string]string{
				"instance_type": "bn",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseClusterName(tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractAlphaPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"lfh02", "lfh"},
		{"bn01", "bn"},
		{"abc", "abc"},
		{"123", ""},
		{"a1b2", "a"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractAlphaPrefix(tt.input))
		})
	}
}

func TestOpsProfile_Labels(t *testing.T) {
	t.Run("returns all labels including cluster, instance, inventory_name, and ip", func(t *testing.T) {
		result := OpsProfile{}.Labels(LabelInput{
			ClusterName:   "lfh02-previewnet-blocknode",
			DeployProfile: "previewnet",
			MachineIP:     "10.0.0.1",
		})
		assert.Equal(t, map[string]string{
			"cluster":        "lfh02-previewnet-blocknode",
			"environment":    "previewnet",
			"instance":       "lfh02-previewnet-blocknode",
			"instance_type":  "lfh",
			"inventory_name": "lfh02-previewnet-blocknode",
			"ip":             "10.0.0.1",
		}, result)
	})

	t.Run("environment derived from deploy profile", func(t *testing.T) {
		result := OpsProfile{}.Labels(LabelInput{
			ClusterName:   "mycluster",
			DeployProfile: "mainnet",
			MachineIP:     "192.168.1.100",
		})
		assert.Equal(t, map[string]string{
			"cluster":        "mycluster",
			"environment":    "mainnet",
			"instance":       "mycluster",
			"instance_type":  "mycluster",
			"inventory_name": "mycluster",
			"ip":             "192.168.1.100",
		}, result)
	})

	t.Run("instance mirrors cluster name for human-readable dashboards", func(t *testing.T) {
		result := OpsProfile{}.Labels(LabelInput{
			ClusterName:   "lfh00-testnet",
			DeployProfile: "testnet",
		})
		assert.Equal(t, "lfh00-testnet", result["instance"])
		assert.Equal(t, result["inventory_name"], result["instance"])
	})

	t.Run("returns empty map when cluster name is empty", func(t *testing.T) {
		result := OpsProfile{}.Labels(LabelInput{})
		assert.Empty(t, result)
	})

	t.Run("ip label omitted when machineIP is empty", func(t *testing.T) {
		result := OpsProfile{}.Labels(LabelInput{
			ClusterName:   "lfh02-previewnet-blocknode",
			DeployProfile: "previewnet",
		})
		assert.NotContains(t, result, "ip")
		assert.Equal(t, "lfh02-previewnet-blocknode", result["cluster"])
	})
}
