// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"encoding/json"
	"sort"

	"github.com/joomcode/errorx"

	"helm.sh/helm/v3/pkg/cli/values"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// alloyNamespace returns the namespace Alloy deploys into, as recorded in
// the infrastructure catalog. The catalog is embedded and validated, so a
// missing entry would be a build-time programming error.
func alloyNamespace() string {
	return software.MustGetClusterComponent("alloy").Namespace
}

// ConfigMapTemplateData holds data for the ConfigMap template.
type ConfigMapTemplateData struct {
	ConfigMapName string
	Namespace     string
	Modules       []ModuleTemplateData
}

// ModuleTemplateData holds module data for the template.
type ModuleTemplateData struct {
	Name            string
	Filename        string
	IndentedContent string
}

// ConfigMapManifest generates the Alloy ConfigMap manifest.
// Uses the configmap.yaml template file.
func ConfigMapManifest(modules []ModuleConfig) (string, error) {
	// Sort modules by name for consistent output
	sortedModules := make([]ModuleConfig, len(modules))
	copy(sortedModules, modules)
	sort.Slice(sortedModules, func(i, j int) bool {
		return sortedModules[i].Name < sortedModules[j].Name
	})

	// Convert to template data with pre-indented content
	templateModules := make([]ModuleTemplateData, len(sortedModules))
	for i, m := range sortedModules {
		templateModules[i] = ModuleTemplateData{
			Name:            m.Name,
			Filename:        m.Filename,
			IndentedContent: IndentLines(m.Content, "    "),
		}
	}

	data := ConfigMapTemplateData{
		ConfigMapName: ConfigMapName,
		Namespace:     alloyNamespace(),
		Modules:       templateModules,
	}

	return templates.Render("files/alloy/configmap.yaml", data)
}

// BaseHelmValues returns the base Helm values for Alloy installation.
// Configures Alloy to load config from a single config.alloy key in the ConfigMap.
func BaseHelmValues() []string {
	return []string{
		"crds.create=true",
		"alloy.configMap.create=false",
		"alloy.configMap.name=" + ConfigMapName,
		"alloy.configMap.key=config.alloy",
		"alloy.clustering.enabled=false",
		"alloy.enableReporting=false",
		"alloy.mounts.varlog=false",
		"controller.type=daemonset",
		"serviceMonitor.enabled=true",
		// Volume mounts for /var/log
		"alloy.mounts.extra[0].name=varlog",
		"alloy.mounts.extra[0].mountPath=/host/var/log",
		"alloy.mounts.extra[0].readOnly=true",
		"controller.volumes.extra[0].name=varlog",
		"controller.volumes.extra[0].hostPath.path=/var/log",
	}
}

// ChartJSONValues returns the --set-json values specific to an Alloy chart
// version. Every catalog version needs an entry reviewed and render-tested for
// that chart; a version without one is refused rather than given another
// version's values.
func ChartJSONValues(chartVersion string) ([]string, error) {
	switch chartVersion {
	case "1.4.0":
		return nil, nil
	default:
		return nil, errorx.IllegalArgument.New("no Helm values are defined for Alloy chart %s", chartVersion)
	}
}

// HostNetworkHelmValues returns Helm values for enabling host network access.
func HostNetworkHelmValues() []string {
	return []string{
		"controller.hostNetwork=true",
		"controller.dnsPolicy=ClusterFirstWithHostNet",
	}
}

// NodeExporterAppName is the chart's nameOverride. It equals the release name, so
// the chart names the Service, DaemonSet and pods after it. The node-exporter.alloy
// selector, the job label (the Service name) and the readiness check rely on that.
const NodeExporterAppName = "node-exporter"

// nodeExporterExtraArgs replaces the chart's extraArgs list, so it repeats the
// chart's two filesystem excludes before adding our collectors.
var nodeExporterExtraArgs = []string{
	`--collector.filesystem.mount-points-exclude=^/(dev|proc|sys|run/containerd/.+|var/lib/docker/.+|var/lib/kubelet/.+)($|/)`,
	`--collector.filesystem.fs-types-exclude=^(autofs|binfmt_misc|bpf|cgroup2?|configfs|debugfs|devpts|devtmpfs|fusectl|hugetlbfs|iso9660|mqueue|nsfs|overlay|proc|procfs|pstore|rpc_pipefs|securityfs|selinuxfs|squashfs|sysfs|tracefs|erofs)$`,
	"--collector.systemd",
	"--collector.processes",
}

// NodeExporterHelmValues returns the values for the prometheus-community
// prometheus-node-exporter chart. Resources match the legacy chart's "small" preset.
func NodeExporterHelmValues() *values.Options {
	extraArgs, _ := json.Marshal(nodeExporterExtraArgs)

	return &values.Options{
		Values: []string{
			"nameOverride=" + NodeExporterAppName,
			"prometheus.monitor.enabled=true",
			"prometheus.monitor.interval=15s",
			"prometheus.monitor.scrapeTimeout=5s",
			"prometheus.monitor.jobLabel=node",
			"prometheus.monitor.attachMetadata.node=true",
			"resources.requests.cpu=500m",
			"resources.requests.memory=512Mi",
			"resources.requests.ephemeral-storage=50Mi",
			"resources.limits.cpu=750m",
			"resources.limits.memory=768Mi",
			"resources.limits.ephemeral-storage=2Gi",
		},
		JSONValues: []string{"extraArgs=" + string(extraArgs)},
	}
}

// BlockNodeServiceMonitorTemplateData holds data for the block-node ServiceMonitor template.
type BlockNodeServiceMonitorTemplateData struct {
	Namespace string
}

// BlockNodeServiceMonitorManifest generates the ServiceMonitor manifest for block-node.
// This is required for Alloy to discover and scrape block-node metrics.
// The namespace parameter should be the block node's configured namespace.
func BlockNodeServiceMonitorManifest(namespace string) (string, error) {
	data := BlockNodeServiceMonitorTemplateData{
		Namespace: namespace,
	}

	return templates.Render(BlockNodeServiceMonitorPath, data)
}

// NamespaceTemplateData holds data for the namespace template.
type NamespaceTemplateData struct {
	Namespace string
}

// NamespaceManifest generates the Alloy namespace manifest.
// Uses the namespace.yaml template file.
func NamespaceManifest() (string, error) {
	data := NamespaceTemplateData{
		Namespace: alloyNamespace(),
	}

	return templates.Render("files/alloy/namespace.yaml", data)
}
