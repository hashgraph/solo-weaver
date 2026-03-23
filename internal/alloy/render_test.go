// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderModularConfigs(t *testing.T) {
	tests := []struct {
		name                string
		cfg                 models.AlloyConfig
		expectedModules     []string
		unexpectedModules   []string
		expectedModuleCount int
	}{
		{
			name: "no remotes, no block node",
			cfg: models.AlloyConfig{
				ClusterName: "test-cluster",
			},
			expectedModules:     []string{"core"},
			unexpectedModules:   []string{"remotes", "block-node", "agent-metrics", "node-exporter", "kubelet", "syslog"},
			expectedModuleCount: 1,
		},
		{
			name: "block node monitoring without remotes - still skipped",
			cfg: models.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
			},
			expectedModules:     []string{"core"},
			unexpectedModules:   []string{"remotes", "block-node", "agent-metrics", "node-exporter", "kubelet", "syslog"},
			expectedModuleCount: 1,
		},
		{
			name: "with prometheus remote only",
			cfg: models.AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []models.AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "agent-metrics", "node-exporter", "kubelet"},
			unexpectedModules:   []string{"block-node", "syslog"},
			expectedModuleCount: 5,
		},
		{
			name: "with prometheus remote only and block node monitoring - block-node skipped without loki",
			cfg: models.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
				PrometheusRemotes: []models.AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "agent-metrics", "node-exporter", "kubelet"},
			unexpectedModules:   []string{"block-node", "syslog"},
			expectedModuleCount: 5,
		},
		{
			name: "with loki remote only",
			cfg: models.AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []models.AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "syslog"},
			unexpectedModules:   []string{"block-node", "agent-metrics", "node-exporter", "kubelet"},
			expectedModuleCount: 3,
		},
		{
			name: "with loki remote only and block node monitoring - block-node skipped without prometheus",
			cfg: models.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
				LokiRemotes: []models.AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "syslog"},
			unexpectedModules:   []string{"block-node", "agent-metrics", "node-exporter", "kubelet"},
			expectedModuleCount: 3,
		},
		{
			name: "full config - all modules",
			cfg: models.AlloyConfig{
				ClusterName:      "test-cluster",
				MonitorBlockNode: true,
				PrometheusRemotes: []models.AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
				},
				LokiRemotes: []models.AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"},
				},
			},
			expectedModules:     []string{"core", "remotes", "agent-metrics", "node-exporter", "kubelet", "syslog", "block-node"},
			expectedModuleCount: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb, err := NewConfigBuilder(tt.cfg, "")
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

func TestRenderModularConfigs_WithOpsLabelProfile_AllModules(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName:      "lfh02-previewnet-blocknode",
		MonitorBlockNode: true,
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "cloud", URL: "http://prom:9090/api/v1/write", Username: "user", LabelProfile: "ops"},
		},
		LokiRemotes: []models.AlloyRemoteConfig{
			{Name: "cloud", URL: "http://loki:3100/loki/api/v1/push", Username: "user", LabelProfile: "ops"},
		},
	}

	cb, err := NewConfigBuilder(cfg, "previewnet")
	require.NoError(t, err)
	// Ensure machineIP is set deterministically for tests so ops profile always includes the ip label.
	cb.machineIP = "10.0.0.1"
	modules, err := RenderModularConfigs(cb)
	require.NoError(t, err)

	// Helper to find module content by name
	findModule := func(name string) string {
		for _, m := range modules {
			if m.Name == name {
				return m.Content
			}
		}
		return ""
	}

	// Verify derived labels appear as rule blocks in all modules (including syslog via per-remote relabel)
	for _, moduleName := range []string{"agent-metrics", "kubelet", "node-exporter", "block-node", "syslog"} {
		content := findModule(moduleName)
		require.NotEmpty(t, content, "module %s should exist", moduleName)
		assert.Contains(t, content, `target_label = "cluster"`, "module %s should contain cluster rule", moduleName)
		assert.Contains(t, content, `replacement  = "lfh02-previewnet-blocknode"`, "module %s should contain cluster name replacement", moduleName)
		assert.Contains(t, content, `target_label = "environment"`, "module %s should contain environment rule", moduleName)
		assert.Contains(t, content, `replacement  = "previewnet"`, "module %s should contain previewnet replacement", moduleName)
		assert.Contains(t, content, `target_label = "instance_type"`, "module %s should contain instance_type rule", moduleName)
		assert.Contains(t, content, `replacement  = "lfh"`, "module %s should contain lfh replacement", moduleName)
		assert.Contains(t, content, `target_label = "ip"`, "module %s should contain ip rule", moduleName)
		// ops profile should include inventory_name
		assert.Contains(t, content, `target_label = "inventory_name"`, "module %s should contain inventory_name", moduleName)
		// instance label should NOT be present
		assert.NotContains(t, content, `target_label = "instance"`, "module %s should not contain instance rule", moduleName)
	}
}

func TestRenderModularConfigs_WithOpsLabelProfile(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName:      "lfh02-previewnet-blocknode",
		MonitorBlockNode: true,
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "cloud", URL: "http://prom:9090/api/v1/write", Username: "user", LabelProfile: "ops"},
		},
		LokiRemotes: []models.AlloyRemoteConfig{
			{Name: "cloud", URL: "http://loki:3100/loki/api/v1/push", Username: "user", LabelProfile: "ops"},
		},
	}

	cb, err := NewConfigBuilder(cfg, "previewnet")
	require.NoError(t, err)
	// Ensure machineIP is set deterministically for tests so ops profile behavior is stable.
	cb.machineIP = "10.0.0.1"
	modules, err := RenderModularConfigs(cb)
	require.NoError(t, err)

	findModule := func(name string) string {
		for _, m := range modules {
			if m.Name == name {
				return m.Content
			}
		}
		return ""
	}

	// Ops profile should include inventory_name
	for _, moduleName := range []string{"agent-metrics", "kubelet", "node-exporter", "block-node", "syslog"} {
		content := findModule(moduleName)
		require.NotEmpty(t, content, "module %s should exist", moduleName)
		assert.Contains(t, content, `target_label = "inventory_name"`, "module %s should contain inventory_name rule", moduleName)
		assert.Contains(t, content, `replacement  = "lfh02-previewnet-blocknode"`, "module %s should contain full cluster name as inventory_name", moduleName)
	}
}

func TestRenderModularConfigs_DefaultEngProfile(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName: "test-cluster",
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"},
		},
	}

	cb, err := NewConfigBuilder(cfg, "")
	require.NoError(t, err)
	modules, err := RenderModularConfigs(cb)
	require.NoError(t, err)

	for _, m := range modules {
		if m.Name == "agent-metrics" {
			// eng profile (default) should include cluster rule via CustomRules
			assert.Contains(t, m.Content, `target_label = "cluster"`)
			assert.Contains(t, m.Content, `replacement  = "test-cluster"`)
			// No instance/environment/instance_type rules with eng profile
			assert.NotContains(t, m.Content, `target_label = "instance_type"`)
			assert.NotContains(t, m.Content, `target_label = "environment"`)
		}
	}
}

func TestRenderModularConfigs_MixedLabelProfiles(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName:      "lfh02-previewnet-blocknode",
		MonitorBlockNode: true,
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "local", URL: "http://prom:9090/api/v1/write", Username: "admin", LabelProfile: "ops"},
			{Name: "backup", URL: "http://prom2:9090/api/v1/write", Username: "admin", LabelProfile: "eng"},
		},
		LokiRemotes: []models.AlloyRemoteConfig{
			{Name: "local", URL: "http://loki:3100/loki/api/v1/push", Username: "admin", LabelProfile: "ops"},
			{Name: "backup", URL: "http://loki2:3100/loki/api/v1/push", Username: "admin", LabelProfile: "eng"},
		},
	}

	cb, err := NewConfigBuilder(cfg, "previewnet")
	require.NoError(t, err)

	// Verify per-remote label resolution via ToTemplateRemotes
	promRemotes, lokiRemotes := cb.ToTemplateRemotes()
	require.Len(t, promRemotes, 2)
	require.Len(t, lokiRemotes, 2)

	// ops remote should have full ops labels (inventory_name, environment, etc.)
	assert.Contains(t, promRemotes[0].CustomRules, `target_label = "inventory_name"`)
	assert.Contains(t, promRemotes[0].CustomRules, `target_label = "environment"`)

	// eng remote should have only cluster label
	assert.Contains(t, promRemotes[1].CustomRules, `target_label = "cluster"`)
	assert.NotContains(t, promRemotes[1].CustomRules, `target_label = "inventory_name"`)

	// Same for Loki remotes
	assert.Contains(t, lokiRemotes[0].CustomRules, `target_label = "inventory_name"`)
	assert.NotContains(t, lokiRemotes[1].CustomRules, `target_label = "inventory_name"`)

	// Verify rendered modules contain both profiles' rules
	modules, err := RenderModularConfigs(cb)
	require.NoError(t, err)

	findModule := func(name string) string {
		for _, m := range modules {
			if m.Name == name {
				return m.Content
			}
		}
		return ""
	}

	// agent-metrics should have both relabel blocks with different label sets
	agentMetrics := findModule("agent-metrics")
	require.NotEmpty(t, agentMetrics)
	assert.Contains(t, agentMetrics, `prometheus.relabel "alloy_local"`)
	assert.Contains(t, agentMetrics, `prometheus.relabel "alloy_backup"`)
}

func TestConfigMapManifest(t *testing.T) {
	modules := []ModuleConfig{
		{Name: "core", Filename: "models.alloy", Content: "// core config"},
		{Name: "remotes", Filename: "remotes.alloy", Content: "// remotes config"},
		{Name: "block-node", Filename: "block-node.alloy", Content: "// block node config"},
	}

	manifest, err := ConfigMapManifest(modules)
	require.NoError(t, err)

	// Verify it's a valid YAML-ish structure
	assert.Contains(t, manifest, "apiVersion: v1")
	assert.Contains(t, manifest, "kind: ConfigMap")
	assert.Contains(t, manifest, "name: "+ConfigMapName)
	assert.Contains(t, manifest, "namespace: "+deps.ALLOY_NAMESPACE)

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
		{Name: "core", Filename: "models.alloy", Content: "content"},
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
	assert.Contains(t, manifest, "name: "+deps.ALLOY_NAMESPACE)
}

func TestBuildHelmEnvVars_DefaultSecret(t *testing.T) {
	cfg := models.AlloyConfig{
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user1"},
		},
		LokiRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user1"},
		},
	}

	envVars := BuildHelmEnvVars(cfg)

	// All env vars should reference the convention-based grafana-alloy-secrets
	assert.Contains(t, envVars, "alloy.extraEnv[0].name=PROMETHEUS_PASSWORD_PRIMARY")
	assert.Contains(t, envVars, "alloy.extraEnv[0].valueFrom.secretKeyRef.name="+SecretsName)
	assert.Contains(t, envVars, "alloy.extraEnv[0].valueFrom.secretKeyRef.key=PROMETHEUS_PASSWORD_PRIMARY")
	assert.Contains(t, envVars, "alloy.extraEnv[1].name=LOKI_PASSWORD_PRIMARY")
	assert.Contains(t, envVars, "alloy.extraEnv[1].valueFrom.secretKeyRef.name="+SecretsName)
	assert.Contains(t, envVars, "alloy.extraEnv[1].valueFrom.secretKeyRef.key=LOKI_PASSWORD_PRIMARY")
}

func TestBuildHelmEnvVars_MultipleRemotes(t *testing.T) {
	cfg := models.AlloyConfig{
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://prom1:9090/api/v1/write", Username: "user1"},
			{Name: "backup", URL: "http://prom2:9090/api/v1/write", Username: "user2"},
		},
	}

	envVars := BuildHelmEnvVars(cfg)

	// Both remotes reference the same conventional secret with derived keys
	assert.Contains(t, envVars, "alloy.extraEnv[0].name=PROMETHEUS_PASSWORD_PRIMARY")
	assert.Contains(t, envVars, "alloy.extraEnv[0].valueFrom.secretKeyRef.name="+SecretsName)
	assert.Contains(t, envVars, "alloy.extraEnv[0].valueFrom.secretKeyRef.key=PROMETHEUS_PASSWORD_PRIMARY")
	assert.Contains(t, envVars, "alloy.extraEnv[1].name=PROMETHEUS_PASSWORD_BACKUP")
	assert.Contains(t, envVars, "alloy.extraEnv[1].valueFrom.secretKeyRef.name="+SecretsName)
	assert.Contains(t, envVars, "alloy.extraEnv[1].valueFrom.secretKeyRef.key=PROMETHEUS_PASSWORD_BACKUP")
}

func TestRequiredSecrets(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName: "test-cluster",
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user1"},
		},
		LokiRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user1"},
		},
	}

	cb, err := NewConfigBuilder(cfg, "")
	require.NoError(t, err)

	secrets := cb.RequiredSecrets()

	// All keys should be under the single conventional secret
	require.Contains(t, secrets, SecretsName)
	assert.Contains(t, secrets[SecretsName], "PROMETHEUS_PASSWORD_PRIMARY")
	assert.Contains(t, secrets[SecretsName], "LOKI_PASSWORD_PRIMARY")
	assert.Len(t, secrets, 1, "should only reference one secret")
}

func TestRequiredSecrets_NoRemotes(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName: "test-cluster",
	}

	cb, err := NewConfigBuilder(cfg, "")
	require.NoError(t, err)

	secrets := cb.RequiredSecrets()
	assert.Nil(t, secrets, "should return nil when no remotes configured")
}

func TestRequiredSecrets_MultipleRemotes(t *testing.T) {
	cfg := models.AlloyConfig{
		ClusterName: "test-cluster",
		PrometheusRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://prom1:9090/api/v1/write", Username: "user1"},
			{Name: "backup", URL: "http://prom2:9090/api/v1/write", Username: "user2"},
		},
		LokiRemotes: []models.AlloyRemoteConfig{
			{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user1"},
		},
	}

	cb, err := NewConfigBuilder(cfg, "")
	require.NoError(t, err)

	secrets := cb.RequiredSecrets()

	require.Contains(t, secrets, SecretsName)
	assert.Len(t, secrets[SecretsName], 3)
	assert.Contains(t, secrets[SecretsName], "PROMETHEUS_PASSWORD_PRIMARY")
	assert.Contains(t, secrets[SecretsName], "PROMETHEUS_PASSWORD_BACKUP")
	assert.Contains(t, secrets[SecretsName], "LOKI_PASSWORD_PRIMARY")
}
