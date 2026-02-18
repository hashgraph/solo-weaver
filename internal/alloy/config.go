// SPDX-License-Identifier: Apache-2.0

// Package alloy provides business logic for Grafana Alloy configuration and installation.
package alloy

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/templates"
)

const (
	// Kubernetes resource names
	Namespace              = "grafana-alloy"
	Release                = "grafana-alloy"
	Chart                  = "grafana/alloy"
	Version                = "1.4.0"
	Repo                   = "https://grafana.github.io/helm-charts"
	ConfigMapName          = "grafana-alloy-cm"
	SecretsName            = "grafana-alloy-secrets"
	ExternalSecretName     = "grafana-alloy-external-secret"
	ClusterSecretStoreName = "vault-secret-store"

	// Node exporter settings
	NodeExporterNamespace = "node-exporter"
	NodeExporterRelease   = "node-exporter"
	NodeExporterChart     = "oci://registry-1.docker.io/bitnamicharts/node-exporter"
	NodeExporterVersion   = "4.5.19"

	// Template paths
	CoreTemplatePath            = "files/alloy/core.alloy"
	RemotesTemplatePath         = "files/alloy/remotes.alloy"
	AgentMetricsTemplatePath    = "files/alloy/agent-metrics.alloy"
	NodeExporterTemplatePath    = "files/alloy/node-exporter.alloy"
	KubeletTemplatePath         = "files/alloy/kubelet.alloy"
	SyslogTemplatePath          = "files/alloy/syslog.alloy"
	BlockNodeTemplatePath       = "files/alloy/block-node.alloy"
	BlockNodeServiceMonitorPath = "files/alloy/block-node-servicemonitor.yaml"
	BlockNodePodLogsPath        = "files/alloy/block-node-podlogs.yaml"

	// Vault path prefix for secrets
	VaultPathPrefix = "grafana/alloy/"
)

// Remote represents a single remote endpoint for Prometheus or Loki.
type Remote struct {
	Name           string
	URL            string
	Username       string
	PasswordEnvVar string
}

// ConfigBuilder helps build Alloy configuration from various sources.
type ConfigBuilder struct {
	clusterName       string
	prometheusRemotes []Remote
	lokiRemotes       []Remote
	monitorBlockNode  bool
}

// NewConfigBuilder creates a new ConfigBuilder from the application config.
// Returns an error if cluster name cannot be determined (neither provided nor hostname available).
func NewConfigBuilder(cfg config.AlloyConfig) (*ConfigBuilder, error) {
	cb := &ConfigBuilder{
		monitorBlockNode: cfg.MonitorBlockNode,
	}

	// Determine cluster name
	cb.clusterName = cfg.ClusterName
	if cb.clusterName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("cluster name not provided and failed to get hostname: %w", err)
		}
		cb.clusterName = hostname
	}

	// Build Prometheus remotes
	cb.prometheusRemotes = buildPrometheusRemotes(cfg)

	// Build Loki remotes
	cb.lokiRemotes = buildLokiRemotes(cfg)

	return cb, nil
}

// ClusterName returns the cluster name.
func (cb *ConfigBuilder) ClusterName() string {
	return cb.clusterName
}

// PrometheusRemotes returns the Prometheus remote configurations.
func (cb *ConfigBuilder) PrometheusRemotes() []Remote {
	return cb.prometheusRemotes
}

// LokiRemotes returns the Loki remote configurations.
func (cb *ConfigBuilder) LokiRemotes() []Remote {
	return cb.lokiRemotes
}

// MonitorBlockNode returns whether block node monitoring is enabled.
func (cb *ConfigBuilder) MonitorBlockNode() bool {
	return cb.monitorBlockNode
}

// PrometheusForwardTo returns the forward_to string for Prometheus remotes.
func (cb *ConfigBuilder) PrometheusForwardTo() string {
	var receivers []string
	for _, r := range cb.prometheusRemotes {
		receivers = append(receivers, "prometheus.remote_write."+r.Name+".receiver")
	}
	return strings.Join(receivers, ", ")
}

// LokiForwardTo returns the forward_to string for Loki remotes.
func (cb *ConfigBuilder) LokiForwardTo() string {
	var receivers []string
	for _, r := range cb.lokiRemotes {
		receivers = append(receivers, "loki.write."+r.Name+".receiver")
	}
	return strings.Join(receivers, ", ")
}

// ToTemplateRemotes converts internal remotes to template remotes.
func (cb *ConfigBuilder) ToTemplateRemotes() ([]templates.AlloyRemote, []templates.AlloyRemote) {
	promRemotes := make([]templates.AlloyRemote, len(cb.prometheusRemotes))
	for i, r := range cb.prometheusRemotes {
		promRemotes[i] = templates.AlloyRemote{
			Name:           r.Name,
			URL:            r.URL,
			Username:       r.Username,
			PasswordEnvVar: r.PasswordEnvVar,
		}
	}

	lokiRemotes := make([]templates.AlloyRemote, len(cb.lokiRemotes))
	for i, r := range cb.lokiRemotes {
		lokiRemotes[i] = templates.AlloyRemote{
			Name:           r.Name,
			URL:            r.URL,
			Username:       r.Username,
			PasswordEnvVar: r.PasswordEnvVar,
		}
	}

	return promRemotes, lokiRemotes
}

// ShouldUseHostNetwork checks if any remote URL requires host network access.
func (cb *ConfigBuilder) ShouldUseHostNetwork() bool {
	for _, r := range cb.prometheusRemotes {
		if isLocalhostURL(r.URL) {
			return true
		}
	}
	for _, r := range cb.lokiRemotes {
		if isLocalhostURL(r.URL) {
			return true
		}
	}
	return false
}

// buildPrometheusRemotes converts config to internal remote format.
func buildPrometheusRemotes(cfg config.AlloyConfig) []Remote {
	var remotes []Remote

	if len(cfg.PrometheusRemotes) > 0 {
		for _, r := range cfg.PrometheusRemotes {
			remotes = append(remotes, Remote{
				Name:           r.Name,
				URL:            r.URL,
				Username:       r.Username,
				PasswordEnvVar: "PROMETHEUS_PASSWORD_" + toEnvVarName(r.Name),
			})
		}
	} else if cfg.PrometheusURL != "" {
		// Backward compatibility: use legacy single remote config
		remotes = append(remotes, Remote{
			Name:           "primary",
			URL:            cfg.PrometheusURL,
			Username:       cfg.PrometheusUsername,
			PasswordEnvVar: "PROMETHEUS_PASSWORD",
		})
	}

	return remotes
}

// buildLokiRemotes converts config to internal remote format.
func buildLokiRemotes(cfg config.AlloyConfig) []Remote {
	var remotes []Remote

	if len(cfg.LokiRemotes) > 0 {
		for _, r := range cfg.LokiRemotes {
			remotes = append(remotes, Remote{
				Name:           r.Name,
				URL:            r.URL,
				Username:       r.Username,
				PasswordEnvVar: "LOKI_PASSWORD_" + toEnvVarName(r.Name),
			})
		}
	} else if cfg.LokiURL != "" {
		// Backward compatibility: use legacy single remote config
		remotes = append(remotes, Remote{
			Name:           "primary",
			URL:            cfg.LokiURL,
			Username:       cfg.LokiUsername,
			PasswordEnvVar: "LOKI_PASSWORD",
		})
	}

	return remotes
}

// toEnvVarName converts a name to a valid Kubernetes environment variable name.
// Env var names must match [A-Z0-9_]+ and cannot start with a digit.
func toEnvVarName(name string) string {
	// Replace dashes with underscores and convert to uppercase
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// isLocalhostURL checks if the URL points to localhost.
func isLocalhostURL(url string) bool {
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1")
}
