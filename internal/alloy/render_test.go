// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderModularConfigs(t *testing.T) {
	tests := []struct {
		name                string
		cfg                 config.AlloyConfig
		expectedModules     []string
		unexpectedModules   []string
		expectedModuleCount int
	}{
		{
			name: "local-only mode - no remotes, no block node",
			cfg: config.AlloyConfig{
				ClusterName: "test-cluster",
			},
			expectedModules:     []string{"core"},
			unexpectedModules:   []string{"remotes", "block-node", "agent-metrics", "node-exporter", "kubelet", "syslog"},
			expectedModuleCount: 1,
		},
		{
			name: "local-only mode with block node monitoring - still skipped without remotes",
			cfg: config.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
			},
			expectedModules:     []string{"core"},
			unexpectedModules:   []string{"remotes", "block-node", "agent-metrics", "node-exporter", "kubelet", "syslog"},
			expectedModuleCount: 1,
		},
		{
			name: "with prometheus remote only",
			cfg: config.AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []config.AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "agent-metrics", "node-exporter", "kubelet"},
			unexpectedModules:   []string{"block-node", "syslog"},
			expectedModuleCount: 5,
		},
		{
			name: "with prometheus remote only and block node monitoring - block-node skipped without loki",
			cfg: config.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
				PrometheusRemotes: []config.AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "agent-metrics", "node-exporter", "kubelet"},
			unexpectedModules:   []string{"block-node", "syslog"},
			expectedModuleCount: 5,
		},
		{
			name: "with loki remote only",
			cfg: config.AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []config.AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "syslog"},
			unexpectedModules:   []string{"block-node", "agent-metrics", "node-exporter", "kubelet"},
			expectedModuleCount: 3,
		},
		{
			name: "with loki remote only and block node monitoring - block-node skipped without prometheus",
			cfg: config.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
				LokiRemotes: []config.AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "syslog"},
			unexpectedModules:   []string{"block-node", "agent-metrics", "node-exporter", "kubelet"},
			expectedModuleCount: 3,
		},
		{
			name: "full config - all modules",
			cfg: config.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
				PrometheusRemotes: []config.AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
				},
				LokiRemotes: []config.AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "agent-metrics", "node-exporter", "kubelet", "syslog", "block-node"},
			expectedModuleCount: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb, err := NewConfigBuilder(tt.cfg)
			require.NoError(t, err)
			modules, err := RenderModularConfigs(cb)
			require.NoError(t, err)

			// Check module count
			assert.Len(t, modules, tt.expectedModuleCount)

			// Check expected modules are present
			moduleNames := GetModuleNames(modules)
			for _, expected := range tt.expectedModules {
				assert.Contains(t, moduleNames, expected, "expected module %s to be present", expected)
			}

			// Check unexpected modules are not present
			for _, unexpected := range tt.unexpectedModules {
				assert.NotContains(t, moduleNames, unexpected, "unexpected module %s should not be present", unexpected)
			}

			// Verify each module has content
			for _, m := range modules {
				assert.NotEmpty(t, m.Name, "module name should not be empty")
				assert.NotEmpty(t, m.Filename, "module filename should not be empty")
				assert.NotEmpty(t, m.Content, "module content should not be empty")
				assert.True(t, strings.HasSuffix(m.Filename, ".alloy"), "filename should end with .alloy")
			}
		})
	}
}

func TestConfigMapManifest(t *testing.T) {
	modules := []ModuleConfig{
		{Name: "core", Filename: "core.alloy", Content: "// core config"},
		{Name: "remotes", Filename: "remotes.alloy", Content: "// remotes config"},
		{Name: "block-node", Filename: "block-node.alloy", Content: "// block node config"},
	}

	manifest, err := ConfigMapManifest(modules)
	require.NoError(t, err)

	// Verify it's a valid YAML-ish structure
	assert.Contains(t, manifest, "apiVersion: v1")
	assert.Contains(t, manifest, "kind: ConfigMap")
	assert.Contains(t, manifest, "name: "+ConfigMapName)
	assert.Contains(t, manifest, "namespace: "+Namespace)

	// Verify main config.alloy is present with concatenated content
	assert.Contains(t, manifest, "config.alloy: |")
	assert.Contains(t, manifest, "// Module: block-node")
	assert.Contains(t, manifest, "// Module: core")
	assert.Contains(t, manifest, "// Module: remotes")

	// Verify content is present
	assert.Contains(t, manifest, "// core config")
	assert.Contains(t, manifest, "// remotes config")
	assert.Contains(t, manifest, "// block node config")
}

func TestConfigMapManifest_Sorted(t *testing.T) {
	// Modules in unsorted order
	modules := []ModuleConfig{
		{Name: "zebra", Filename: "zebra.alloy", Content: "// zebra"},
		{Name: "alpha", Filename: "alpha.alloy", Content: "// alpha"},
		{Name: "mid", Filename: "mid.alloy", Content: "// mid"},
	}

	manifest, err := ConfigMapManifest(modules)
	require.NoError(t, err)

	// Find positions of each module header in the main config.alloy section
	alphaPos := strings.Index(manifest, "// Module: alpha")
	midPos := strings.Index(manifest, "// Module: mid")
	zebraPos := strings.Index(manifest, "// Module: zebra")

	// Verify alphabetical order
	require.Greater(t, alphaPos, 0, "alpha module should be in manifest")
	require.Greater(t, midPos, 0, "mid module should be in manifest")
	require.Greater(t, zebraPos, 0, "zebra module should be in manifest")

	assert.Less(t, alphaPos, midPos, "alpha should come before mid")
	assert.Less(t, midPos, zebraPos, "mid should come before zebra")
}

func TestGetModuleNames(t *testing.T) {
	modules := []ModuleConfig{
		{Name: "core", Filename: "core.alloy", Content: "content"},
		{Name: "remotes", Filename: "remotes.alloy", Content: "content"},
	}

	names := GetModuleNames(modules)

	assert.Equal(t, []string{"core", "remotes"}, names)
}

func TestGetModuleNames_Empty(t *testing.T) {
	modules := []ModuleConfig{}
	names := GetModuleNames(modules)
	assert.Empty(t, names)
}

func TestNamespaceManifest(t *testing.T) {
	manifest, err := NamespaceManifest()
	require.NoError(t, err)

	// Verify it's a valid namespace manifest
	assert.Contains(t, manifest, "apiVersion: v1")
	assert.Contains(t, manifest, "kind: Namespace")
	assert.Contains(t, manifest, "name: "+Namespace)
}

func TestEmptySecretManifest(t *testing.T) {
	manifest, err := EmptySecretManifest()
	require.NoError(t, err)

	// Verify it's a valid secret manifest
	assert.Contains(t, manifest, "apiVersion: v1")
	assert.Contains(t, manifest, "kind: Secret")
	assert.Contains(t, manifest, "name: "+SecretsName)
	assert.Contains(t, manifest, "namespace: "+Namespace)
	assert.Contains(t, manifest, "type: Opaque")
	assert.Contains(t, manifest, "data: {}")
}
