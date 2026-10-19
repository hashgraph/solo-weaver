// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/getter"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

func mergedNodeExporterValues(t *testing.T) map[string]any {
	t.Helper()
	vals, err := NodeExporterHelmValues().MergeValues(getter.Providers{})
	require.NoError(t, err)
	return vals
}

func TestNodeExporterHelmValues_ServiceMonitor(t *testing.T) {
	vals := mergedNodeExporterValues(t)

	assert.Equal(t, map[string]any{
		"monitor": map[string]any{
			"enabled":        true,
			"interval":       "15s",
			"scrapeTimeout":  "5s",
			"jobLabel":       "node",
			"attachMetadata": map[string]any{"node": true},
		},
	}, vals["prometheus"])
}

func TestNodeExporterHelmValues_ResourcesMatchLegacySmallPreset(t *testing.T) {
	vals := mergedNodeExporterValues(t)

	assert.Equal(t, map[string]any{
		"requests": map[string]any{"cpu": "500m", "memory": "512Mi", "ephemeral-storage": "50Mi"},
		"limits":   map[string]any{"cpu": "750m", "memory": "768Mi", "ephemeral-storage": "2Gi"},
	}, vals["resources"])
}

func TestNodeExporterHelmValues_ExtraArgsKeepChartDefaults(t *testing.T) {
	vals := mergedNodeExporterValues(t)

	args, ok := vals["extraArgs"].([]any)
	require.True(t, ok, "extraArgs must be a list for the prometheus-community chart, got %T", vals["extraArgs"])
	assert.Equal(t, []any{
		"--collector.filesystem.mount-points-exclude=^/(dev|proc|sys|run/containerd/.+|var/lib/docker/.+|var/lib/kubelet/.+)($|/)",
		"--collector.filesystem.fs-types-exclude=^(autofs|binfmt_misc|bpf|cgroup2?|configfs|debugfs|devpts|devtmpfs|fusectl|hugetlbfs|iso9660|mqueue|nsfs|overlay|proc|procfs|pstore|rpc_pipefs|securityfs|selinuxfs|squashfs|sysfs|tracefs|erofs)$",
		"--collector.systemd",
		"--collector.processes",
	}, args)
}

func TestNodeExporterHelmValues_NoLegacyChartKeys(t *testing.T) {
	vals := mergedNodeExporterValues(t)

	for _, key := range []string{"serviceMonitor", "resourcesPreset", "resourceType", "rbac", "image"} {
		assert.NotContains(t, vals, key)
	}
}

// The chart names its DaemonSet, pods and Service after the release when the
// release contains nameOverride. That keeps the pod prefix the readiness check
// waits on and the Service name Alloy uses as the job label.
func TestNodeExporterHelmValues_NamingMatchesReleaseAndAlloySelector(t *testing.T) {
	vals := mergedNodeExporterValues(t)
	assert.Equal(t, NodeExporterAppName, vals["nameOverride"])

	chart := software.MustGetClusterComponent("node-exporter")
	assert.Equal(t, NodeExporterAppName, chart.Release)

	cfg, err := templates.Render(NodeExporterTemplatePath, templates.AlloyData{
		PrometheusRemotes: []templates.AlloyRemote{{Name: "primary"}},
	})
	require.NoError(t, err)
	assert.Contains(t, cfg, `namespaces = ["`+chart.Namespace+`"]`)
	assert.Contains(t, cfg, `key      = "app.kubernetes.io/name"`)
	assert.Contains(t, cfg, `values   = ["`+NodeExporterAppName+`"]`)
}
