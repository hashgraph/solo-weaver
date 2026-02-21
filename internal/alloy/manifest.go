// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"sort"

	"github.com/hashgraph/solo-weaver/internal/templates"
)

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
		Namespace:     Namespace,
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

// HostNetworkHelmValues returns Helm values for enabling host network access.
func HostNetworkHelmValues() []string {
	return []string{
		"controller.hostNetwork=true",
		"controller.dnsPolicy=ClusterFirstWithHostNet",
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

// BlockNodePodLogsTemplateData holds data for the block-node PodLogs template.
type BlockNodePodLogsTemplateData struct {
	Namespace string
}

// BlockNodePodLogsManifest generates the PodLogs manifest for block-node.
// This is required for Alloy to discover and collect block-node logs.
// The namespace parameter should be the block node's configured namespace.
func BlockNodePodLogsManifest(namespace string) (string, error) {
	data := BlockNodePodLogsTemplateData{
		Namespace: namespace,
	}

	return templates.Render(BlockNodePodLogsPath, data)
}

// NamespaceTemplateData holds data for the namespace template.
type NamespaceTemplateData struct {
	Namespace string
}

// NamespaceManifest generates the Alloy namespace manifest.
// Uses the namespace.yaml template file.
func NamespaceManifest() (string, error) {
	data := NamespaceTemplateData{
		Namespace: Namespace,
	}

	return templates.Render("files/alloy/namespace.yaml", data)
}
