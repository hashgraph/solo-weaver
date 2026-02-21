// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"

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

Passwords for remote endpoints must exist in K8s Secret "grafana-alloy-secrets" 
in the "grafana-alloy" namespace before running this command. Use 
"solo-provisioner eso secret create" to create the secret from an external store,
or create it manually.

Examples:
  # Step 1: Create the K8s secret with passwords (via ESO)
  solo-provisioner eso secret create \
    --store=vault-store \
    --name=grafana-alloy-secrets \
    --namespace=grafana-alloy \
    --set PROMETHEUS_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/prometheus/primary#password \
    --set LOKI_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/loki/primary#password

  # Step 2: Install Alloy with remotes
  solo-provisioner alloy cluster install \
    --cluster-name=my-cluster \
    --add-prometheus-remote=name=primary,url=https://prom1.example.com/api/v1/write,username=user1 \
    --add-prometheus-remote=name=backup,url=https://prom2.example.com/api/v1/write,username=user2 \
    --add-loki-remote=name=primary,url=https://loki1.example.com/loki/api/v1/push,username=user1 \
    --monitor-block-node

  # Local-only mode (no remotes, no secret needed)
  solo-provisioner alloy cluster install \
    --cluster-name=my-cluster`,
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

		// Validate that legacy and new flags are not mixed
		hasLegacyFlags := flagPrometheusURL != "" || flagPrometheusUsername != "" || flagLokiURL != "" || flagLokiUsername != ""
		hasNewFlags := len(prometheusRemotes) > 0 || len(lokiRemotes) > 0

		if hasLegacyFlags && hasNewFlags {
			return fmt.Errorf("cannot mix legacy flags (--prometheus-url, --prometheus-username, --loki-url, --loki-username) with new flags (--add-prometheus-remote, --add-loki-remote); use one style or the other")
		}

		// Apply Alloy configuration overrides
		alloyOverrides := config.AlloyConfig{
			MonitorBlockNode:       flagMonitorBlockNode,
			ClusterName:            flagClusterName,
			ClusterSecretStoreName: flagClusterSecretStore,
			PrometheusRemotes:      prometheusRemotes,
			LokiRemotes:            lokiRemotes,
			// Legacy single-remote flags (for backward compatibility)
			PrometheusURL:      flagPrometheusURL,
			PrometheusUsername: flagPrometheusUsername,
			LokiURL:            flagLokiURL,
			LokiUsername:       flagLokiUsername,
		}

		// Validate configuration before proceeding
		if err := alloyOverrides.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
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
		if hasLegacyFlags {
			l.Warn().Msg("--prometheus-url, --prometheus-username, --loki-url, and --loki-username flags are deprecated; please use --add-prometheus-remote and --add-loki-remote instead")
		}

		wb := workflows.NewAlloyInstallWorkflow()

		common.RunWorkflow(cmd.Context(), wb)

		l.Info().Msg("Successfully installed Alloy observability stack")
		return nil
	},
}

// Verify that the required step functions are accessible
var _ = steps.SetupAlloyStack
