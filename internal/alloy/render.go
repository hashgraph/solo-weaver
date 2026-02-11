// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/templates"
)

// RenderModularConfigWithModules renders all modular Alloy configuration files into a single config
// and returns the list of modules that were loaded.
func RenderModularConfigWithModules(cb *ConfigBuilder) (string, []string) {
	var configParts []string
	var modules []string

	prometheusRemotes, lokiRemotes := cb.ToTemplateRemotes()
	prometheusForwardTo := cb.PrometheusForwardTo()
	lokiForwardTo := cb.LokiForwardTo()

	// 1. Core config (always included)
	coreConfig, err := templates.Render(CoreTemplatePath, nil)
	if err == nil {
		configParts = append(configParts, coreConfig)
		modules = append(modules, "core")
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
			configParts = append(configParts, remotesConfig)
			modules = append(modules, "remotes")
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
		configParts = append(configParts, agentMetricsConfig)
		modules = append(modules, "agent-metrics")
	}

	// 4. Node exporter config (always included)
	nodeExporterConfig, err := templates.Render(NodeExporterTemplatePath, moduleData)
	if err == nil {
		configParts = append(configParts, nodeExporterConfig)
		modules = append(modules, "node-exporter")
	}

	// 5. Kubelet/cAdvisor config (always included)
	kubeletConfig, err := templates.Render(KubeletTemplatePath, moduleData)
	if err == nil {
		configParts = append(configParts, kubeletConfig)
		modules = append(modules, "kubelet")
	}

	// 6. Syslog config (always included)
	syslogConfig, err := templates.Render(SyslogTemplatePath, moduleData)
	if err == nil {
		configParts = append(configParts, syslogConfig)
		modules = append(modules, "syslog")
	}

	// 7. Block node config (conditional)
	if cb.MonitorBlockNode() {
		blockNodeConfig, err := templates.Render(BlockNodeTemplatePath, moduleData)
		if err == nil {
			configParts = append(configParts, blockNodeConfig)
			modules = append(modules, "block-node")
		}
	}

	return strings.Join(configParts, "\n"), modules
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
