// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"fmt"
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
// Returns an error if any required template fails to render.
func RenderModularConfigs(cb *ConfigBuilder) ([]ModuleConfig, error) {
	var modules []ModuleConfig

	prometheusRemotes, lokiRemotes := cb.ToTemplateRemotes()
	prometheusForwardTo := cb.PrometheusForwardTo()
	lokiForwardTo := cb.LokiForwardTo()

	hasPrometheusRemotes := prometheusForwardTo != ""
	hasLokiRemotes := lokiForwardTo != ""

	// 1. Core config (always required)
	coreConfig, err := templates.Render(CoreTemplatePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to render core config: %w", err)
	}
	modules = append(modules, ModuleConfig{
		Name:     "core",
		Filename: "core.alloy",
		Content:  coreConfig,
	})

	// 2. Remotes config (only if remotes are configured)
	if len(prometheusRemotes) > 0 || len(lokiRemotes) > 0 {
		remotesData := templates.AlloyData{
			ClusterName:       cb.ClusterName(),
			PrometheusRemotes: prometheusRemotes,
			LokiRemotes:       lokiRemotes,
		}
		remotesConfig, err := templates.Render(RemotesTemplatePath, remotesData)
		if err != nil {
			return nil, fmt.Errorf("failed to render remotes config: %w", err)
		}
		modules = append(modules, ModuleConfig{
			Name:     "remotes",
			Filename: "remotes.alloy",
			Content:  remotesConfig,
		})
	}

	// Common module data for most modules
	moduleData := templates.AlloyModuleData{
		ClusterName:         cb.ClusterName(),
		PrometheusForwardTo: prometheusForwardTo,
		LokiForwardTo:       lokiForwardTo,
	}

	// 3. Agent metrics config (only if Prometheus remotes are configured)
	if hasPrometheusRemotes {
		agentMetricsConfig, err := templates.Render(AgentMetricsTemplatePath, moduleData)
		if err != nil {
			return nil, fmt.Errorf("failed to render agent-metrics config: %w", err)
		}
		modules = append(modules, ModuleConfig{
			Name:     "agent-metrics",
			Filename: "agent-metrics.alloy",
			Content:  agentMetricsConfig,
		})
	}

	// 4. Node exporter config (only if Prometheus remotes are configured)
	if hasPrometheusRemotes {
		nodeExporterConfig, err := templates.Render(NodeExporterTemplatePath, moduleData)
		if err != nil {
			return nil, fmt.Errorf("failed to render node-exporter config: %w", err)
		}
		modules = append(modules, ModuleConfig{
			Name:     "node-exporter",
			Filename: "node-exporter.alloy",
			Content:  nodeExporterConfig,
		})
	}

	// 5. Kubelet/cAdvisor config (only if Prometheus remotes are configured)
	if hasPrometheusRemotes {
		kubeletConfig, err := templates.Render(KubeletTemplatePath, moduleData)
		if err != nil {
			return nil, fmt.Errorf("failed to render kubelet config: %w", err)
		}
		modules = append(modules, ModuleConfig{
			Name:     "kubelet",
			Filename: "kubelet.alloy",
			Content:  kubeletConfig,
		})
	}

	// 6. Syslog config (only if Loki remotes are configured)
	if hasLokiRemotes {
		syslogConfig, err := templates.Render(SyslogTemplatePath, moduleData)
		if err != nil {
			return nil, fmt.Errorf("failed to render syslog config: %w", err)
		}
		modules = append(modules, ModuleConfig{
			Name:     "syslog",
			Filename: "syslog.alloy",
			Content:  syslogConfig,
		})
	}

	// 7. Block node config (only if monitoring is enabled AND both remote types are configured)
	// Block node monitoring requires both Prometheus (for metrics) and Loki (for logs)
	if cb.MonitorBlockNode() && hasPrometheusRemotes && hasLokiRemotes {
		blockNodeConfig, err := templates.Render(BlockNodeTemplatePath, moduleData)
		if err != nil {
			return nil, fmt.Errorf("failed to render block-node config: %w", err)
		}
		modules = append(modules, ModuleConfig{
			Name:     "block-node",
			Filename: "block-node.alloy",
			Content:  blockNodeConfig,
		})
	}

	return modules, nil
}

// GetModuleNames returns just the module names from the configs.
func GetModuleNames(modules []ModuleConfig) []string {
	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = m.Name
	}
	return names
}

// BuildHelmEnvVars builds the Helm values for environment variables from secrets.
// All passwords are sourced from the conventional K8s Secret "grafana-alloy-secrets"
// using keys derived from remote names (e.g., PROMETHEUS_PASSWORD_PRIMARY).
func BuildHelmEnvVars(cfg config.AlloyConfig) []string {
	var envVars []string
	idx := 0

	// Handle Prometheus remotes
	if len(cfg.PrometheusRemotes) > 0 {
		for _, r := range cfg.PrometheusRemotes {
			envVarName := "PROMETHEUS_PASSWORD_" + toEnvVarName(r.Name)
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
			envVarName := "LOKI_PASSWORD_" + toEnvVarName(r.Name)
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

// buildEnvVarHelmValues builds the Helm value entries for a single environment variable
// referencing the conventional K8s Secret "grafana-alloy-secrets".
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
