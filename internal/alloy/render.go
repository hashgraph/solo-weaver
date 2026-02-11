// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/templates"
)

// ModuleConfig represents a single Alloy configuration module.
type ModuleConfig struct {
	Name     string // Module name (e.g., "core", "remotes", "block-node")
	Filename string // Filename for the ConfigMap key (e.g., "core.alloy")
	Content  string // Rendered template content
}

// RenderModularConfigs renders all Alloy configuration modules as separate configs.
// Returns a slice of ModuleConfig with the module name, filename, and content.
func RenderModularConfigs(cb *ConfigBuilder) []ModuleConfig {
	var modules []ModuleConfig

	prometheusRemotes, lokiRemotes := cb.ToTemplateRemotes()
	prometheusForwardTo := cb.PrometheusForwardTo()
	lokiForwardTo := cb.LokiForwardTo()

	// 1. Core config (always included)
	coreConfig, err := templates.Render(CoreTemplatePath, nil)
	if err == nil {
		modules = append(modules, ModuleConfig{
			Name:     "core",
			Filename: "core.alloy",
			Content:  coreConfig,
		})
	}

	// 2. Remotes config (always included if remotes are configured)
	if len(prometheusRemotes) > 0 || len(lokiRemotes) > 0 {
		remotesData := templates.AlloyData{
			ClusterName:       cb.ClusterName(),
			PrometheusRemotes: prometheusRemotes,
			LokiRemotes:       lokiRemotes,
		}
		remotesConfig, err := templates.Render(RemotesTemplatePath, remotesData)
		if err == nil {
			modules = append(modules, ModuleConfig{
				Name:     "remotes",
				Filename: "remotes.alloy",
				Content:  remotesConfig,
			})
		}
	}

	// Common module data for most modules
	moduleData := templates.AlloyModuleData{
		ClusterName:         cb.ClusterName(),
		PrometheusForwardTo: prometheusForwardTo,
		LokiForwardTo:       lokiForwardTo,
	}

	// 3. Agent metrics config (always included)
	agentMetricsConfig, err := templates.Render(AgentMetricsTemplatePath, moduleData)
	if err == nil {
		modules = append(modules, ModuleConfig{
			Name:     "agent-metrics",
			Filename: "agent-metrics.alloy",
			Content:  agentMetricsConfig,
		})
	}

	// 4. Node exporter config (always included)
	nodeExporterConfig, err := templates.Render(NodeExporterTemplatePath, moduleData)
	if err == nil {
		modules = append(modules, ModuleConfig{
			Name:     "node-exporter",
			Filename: "node-exporter.alloy",
			Content:  nodeExporterConfig,
		})
	}

	// 5. Kubelet/cAdvisor config (always included)
	kubeletConfig, err := templates.Render(KubeletTemplatePath, moduleData)
	if err == nil {
		modules = append(modules, ModuleConfig{
			Name:     "kubelet",
			Filename: "kubelet.alloy",
			Content:  kubeletConfig,
		})
	}

	// 6. Syslog config (always included)
	syslogConfig, err := templates.Render(SyslogTemplatePath, moduleData)
	if err == nil {
		modules = append(modules, ModuleConfig{
			Name:     "syslog",
			Filename: "syslog.alloy",
			Content:  syslogConfig,
		})
	}

	// 7. Block node config (conditional)
	if cb.MonitorBlockNode() {
		blockNodeConfig, err := templates.Render(BlockNodeTemplatePath, moduleData)
		if err == nil {
			modules = append(modules, ModuleConfig{
				Name:     "block-node",
				Filename: "block-node.alloy",
				Content:  blockNodeConfig,
			})
		}
	}

	return modules
}

// GetModuleNames returns just the module names from the configs.
func GetModuleNames(modules []ModuleConfig) []string {
	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = m.Name
	}
	return names
}

// BuildExternalSecretDataEntries builds the data entries for the ExternalSecret manifest.
func BuildExternalSecretDataEntries(cfg config.AlloyConfig, clusterName string) string {
	var entries []string

	// Handle Prometheus remotes
	if len(cfg.PrometheusRemotes) > 0 {
		for _, r := range cfg.PrometheusRemotes {
			envVarName := "PROMETHEUS_PASSWORD_" + strings.ToUpper(r.Name)
			vaultKey := VaultPathPrefix + clusterName + "/prometheus/" + r.Name
			entries = append(entries, buildSecretDataEntry(envVarName, vaultKey, "password"))
		}
	} else if cfg.PrometheusURL != "" {
		// Backward compatibility: legacy single remote
		entries = append(entries, buildSecretDataEntry("PROMETHEUS_PASSWORD", VaultPathPrefix+clusterName+"/prometheus", "password"))
	}

	// Handle Loki remotes
	if len(cfg.LokiRemotes) > 0 {
		for _, r := range cfg.LokiRemotes {
			envVarName := "LOKI_PASSWORD_" + strings.ToUpper(r.Name)
			vaultKey := VaultPathPrefix + clusterName + "/loki/" + r.Name
			entries = append(entries, buildSecretDataEntry(envVarName, vaultKey, "password"))
		}
	} else if cfg.LokiURL != "" {
		// Backward compatibility: legacy single remote
		entries = append(entries, buildSecretDataEntry("LOKI_PASSWORD", VaultPathPrefix+clusterName+"/loki", "password"))
	}

	return strings.Join(entries, "")
}

// buildSecretDataEntry builds a single ExternalSecret data entry.
func buildSecretDataEntry(secretKey, vaultKey, property string) string {
	return `    - secretKey: ` + secretKey + `
      remoteRef:
        key: "` + vaultKey + `"
        property: ` + property + `
`
}

// BuildHelmEnvVars builds the Helm values for environment variables from secrets.
func BuildHelmEnvVars(cfg config.AlloyConfig) []string {
	var envVars []string
	idx := 0

	// Handle Prometheus remotes
	if len(cfg.PrometheusRemotes) > 0 {
		for _, r := range cfg.PrometheusRemotes {
			envVarName := "PROMETHEUS_PASSWORD_" + strings.ToUpper(r.Name)
			envVars = append(envVars, buildEnvVarHelmValues(idx, envVarName)...)
			idx++
		}
	} else if cfg.PrometheusURL != "" {
		// Backward compatibility: legacy single remote
		envVars = append(envVars, buildEnvVarHelmValues(idx, "PROMETHEUS_PASSWORD")...)
		idx++
	}

	// Handle Loki remotes
	if len(cfg.LokiRemotes) > 0 {
		for _, r := range cfg.LokiRemotes {
			envVarName := "LOKI_PASSWORD_" + strings.ToUpper(r.Name)
			envVars = append(envVars, buildEnvVarHelmValues(idx, envVarName)...)
			idx++
		}
	} else if cfg.LokiURL != "" {
		// Backward compatibility: legacy single remote
		envVars = append(envVars, buildEnvVarHelmValues(idx, "LOKI_PASSWORD")...)
		idx++
	}

	return envVars
}

// buildEnvVarHelmValues builds the Helm value entries for a single environment variable.
func buildEnvVarHelmValues(idx int, envVarName string) []string {
	idxStr := strconv.Itoa(idx)
	return []string{
		"alloy.extraEnv[" + idxStr + "].name=" + envVarName,
		"alloy.extraEnv[" + idxStr + "].valueFrom.secretKeyRef.name=" + SecretsName,
		"alloy.extraEnv[" + idxStr + "].valueFrom.secretKeyRef.key=" + envVarName,
	}
}

// IndentLines adds the specified indentation to each line of the text.
func IndentLines(text, indent string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}
