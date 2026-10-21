// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"encoding/json"
	"slices"
	"strings"
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

// Alloy 1.4.0 releases are reconfigured with these values, so they must stay
// what earlier releases deployed.
func TestBaseHelmValues_Unchanged(t *testing.T) {
	assert.Equal(t, []string{
		"crds.create=true",
		"alloy.configMap.create=false",
		"alloy.configMap.name=grafana-alloy-cm",
		"alloy.configMap.key=config.alloy",
		"alloy.clustering.enabled=false",
		"alloy.enableReporting=false",
		"alloy.mounts.varlog=false",
		"controller.type=daemonset",
		"serviceMonitor.enabled=true",
		"alloy.mounts.extra[0].name=varlog",
		"alloy.mounts.extra[0].mountPath=/host/var/log",
		"alloy.mounts.extra[0].readOnly=true",
		"controller.volumes.extra[0].name=varlog",
		"controller.volumes.extra[0].hostPath.path=/var/log",
	}, BaseHelmValues())
}

func TestChartJSONValues_Alloy140KeepsChartDefaults(t *testing.T) {
	vals, err := ChartJSONValues("1.4.0")
	require.NoError(t, err)
	assert.Empty(t, vals)
}

func TestChartJSONValues_Alloy1131RestoresNodesProxyGet(t *testing.T) {
	vals, err := ChartJSONValues("1.13.1")
	require.NoError(t, err)
	require.Len(t, vals, 1)

	key, raw, ok := strings.Cut(vals[0], "=")
	require.True(t, ok)
	assert.Equal(t, "rbac.clusterRules", key)
	var rules []rbacRule
	require.NoError(t, json.Unmarshal([]byte(raw), &rules))
	assert.Contains(t, rules, rbacRule{APIGroups: []string{""}, Resources: []string{"nodes/proxy"}, Verbs: []string{"get"}})
	for _, r := range rules {
		if slices.Contains(r.Resources, "nodes/proxy") {
			assert.Equal(t, []string{"get"}, r.Verbs, "nodes/proxy must be get-only")
		}
	}
}

func TestChartJSONValues_UnknownVersionIsRefused(t *testing.T) {
	for _, v := range []string{"1.5.3", "1.14.0", ""} {
		_, err := ChartJSONValues(v)
		assert.Error(t, err, v)
	}
}

// Every Alloy version the catalog can deploy needs reviewed values.
func TestChartJSONValues_CoverEveryCatalogVersion(t *testing.T) {
	for v := range software.MustGetClusterComponent("alloy").Versions {
		_, err := ChartJSONValues(string(v))
		assert.NoError(t, err, "Alloy chart %s has no reviewed Helm values", v)
	}
}

func TestBlockNodeServiceMonitorName_MatchesTemplate(t *testing.T) {
	manifest, err := BlockNodeServiceMonitorManifest("block-node")
	require.NoError(t, err)
	assert.Contains(t, manifest, "name: "+BlockNodeServiceMonitorName+"\n")
}
