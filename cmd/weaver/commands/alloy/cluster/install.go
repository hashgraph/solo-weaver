// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Grafana Alloy observability stack",
	Long: `Install the Grafana Alloy observability stack including Prometheus CRDs, 
Node Exporter, and Alloy for metrics and logs collection.

Examples:
  # Single remote (legacy mode)
  solo-provisioner alloy cluster install \
    --cluster-name=my-cluster \
    --prometheus-url=https://prometheus.example.com/api/v1/write \
    --prometheus-username=user \
    --loki-url=https://loki.example.com/loki/api/v1/push \
    --loki-username=user

  # Multiple remotes
  solo-provisioner alloy cluster install \
    --cluster-name=my-cluster \
    --add-prometheus-remote=primary:https://prom1.example.com/api/v1/write:user1 \
    --add-prometheus-remote=backup:https://prom2.example.com/api/v1/write:user2 \
    --add-loki-remote=primary:https://loki1.example.com/loki/api/v1/push:user1 \
    --monitor-block-node`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse multi-remote flags
		prometheusRemotes, err := parseRemoteFlags(flagPrometheusRemotes)
		if err != nil {
			return fmt.Errorf("invalid --add-prometheus-remote flag: %w", err)
		}

		lokiRemotes, err := parseRemoteFlags(flagLokiRemotes)
		if err != nil {
			return fmt.Errorf("invalid --add-loki-remote flag: %w", err)
		}

		// Apply Alloy configuration overrides
		alloyOverrides := config.AlloyConfig{
			MonitorBlockNode:  flagMonitorBlockNode,
			ClusterName:       flagClusterName,
			PrometheusRemotes: prometheusRemotes,
			LokiRemotes:       lokiRemotes,
			// Legacy single-remote flags (for backward compatibility)
			PrometheusURL:      flagPrometheusURL,
			PrometheusUsername: flagPrometheusUsername,
			LokiURL:            flagLokiURL,
			LokiUsername:       flagLokiUsername,
		}
		config.OverrideAlloyConfig(alloyOverrides)

		l := logx.As()
		l.Debug().
			Strs("args", args).
			Bool("monitorBlockNode", flagMonitorBlockNode).
			Str("clusterName", flagClusterName).
			Int("prometheusRemotes", len(prometheusRemotes)).
			Int("lokiRemotes", len(lokiRemotes)).
			Msg("Installing Alloy observability stack")

		// Warn about deprecated flags
		if flagPrometheusURL != "" || flagLokiURL != "" {
			l.Warn().Msg("--prometheus-url and --loki-url flags are deprecated. Use --add-prometheus-remote and --add-loki-remote instead")
		}

		wb := workflows.NewAlloyInstallWorkflow()

		common.RunWorkflow(cmd.Context(), wb)

		l.Info().Msg("Successfully installed Alloy observability stack")
		return nil
	},
}

// parseRemoteFlags parses the repeatable remote flags in format "name:url:username"
// Since URLs contain colons (e.g., http://host:port/path), we parse by finding:
// - First colon separates the name
// - Last colon separates the username
// - Everything in between is the URL
func parseRemoteFlags(flags []string) ([]config.AlloyRemoteConfig, error) {
	var remotes []config.AlloyRemoteConfig

	for _, flag := range flags {
		// Find the first colon (separates name from rest)
		firstColon := strings.Index(flag, ":")
		if firstColon == -1 {
			return nil, fmt.Errorf("invalid format %q, expected name:url:username", flag)
		}

		name := strings.TrimSpace(flag[:firstColon])
		rest := flag[firstColon+1:]

		// Find the last colon (separates username from URL)
		lastColon := strings.LastIndex(rest, ":")
		if lastColon == -1 {
			return nil, fmt.Errorf("invalid format %q, expected name:url:username", flag)
		}

		url := strings.TrimSpace(rest[:lastColon])
		username := strings.TrimSpace(rest[lastColon+1:])

		if name == "" {
			return nil, fmt.Errorf("remote name cannot be empty in %q", flag)
		}
		if url == "" {
			return nil, fmt.Errorf("remote URL cannot be empty in %q", flag)
		}

		remotes = append(remotes, config.AlloyRemoteConfig{
			Name:     name,
			URL:      url,
			Username: username,
		})
	}

	return remotes, nil
}

// Verify that the required step functions are accessible
var _ = steps.SetupAlloyStack
