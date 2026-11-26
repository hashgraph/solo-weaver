// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
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

  # Install Alloy without remotes (no secret needed)
  solo-provisioner alloy cluster install \
    --cluster-name=my-cluster`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve the deployment profile. Unlike block/kube this flag is optional:
		// when omitted we leave config.Profile as loaded via the root --config flag
		// (viper) so the environment label still resolves. Only override the global
		// config when a non-empty --profile is explicitly provided, and validate it.
		flagProfile, err := common.FlagProfile().Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}
		if flagProfile != "" {
			if !hardware.IsValidProfile(flagProfile) {
				return errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
					flagProfile, models.SupportedProfiles())
			}
			// Set the profile in the global config so the label profile can emit
			// the environment label on scraped metrics and logs.
			config.SetProfile(flagProfile)
		}

		// Parse multi-remote flags
		prometheusRemotes, err := parseRemoteFlags(flagPrometheusRemotes)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --add-prometheus-remote flag")
		}

		lokiRemotes, err := parseRemoteFlags(flagLokiRemotes)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --add-loki-remote flag")
		}

		alloyFlags := models.AlloyConfig{
			MonitorBlockNode:       flagMonitorBlockNode,
			ClusterName:            flagClusterName,
			Environment:            flagEnvironment,
			ClusterSecretStoreName: flagClusterSecretStore,
			PrometheusRemotes:      prometheusRemotes,
			LokiRemotes:            lokiRemotes,
			// Legacy single-remote flags (for backward compatibility)
			PrometheusURL:      flagPrometheusURL,
			PrometheusUsername: flagPrometheusUsername,
			LokiURL:            flagLokiURL,
			LokiUsername:       flagLokiUsername,
		}

		if err := alloyFlags.Validate(); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid configuration")
		}

		alloyCfg, err := resolveAlloyConfig(config.Get().Alloy, alloyFlags,
			cmd.Flags().Changed(common.FlagMonitorBlockNode().Name))
		if err != nil {
			return err
		}

		// This command never runs common.Setup, so the file's alloy block is
		// checked here. The flags passed above, so what fails here is the file's.
		if err := alloyCfg.Validate(); err != nil {
			return errx.Decorate(errorx.IllegalArgument.Wrap(err, "invalid alloy settings in --config"),
				reasons.InvalidArgument, configFileHint())
		}

		config.OverrideAlloyConfig(alloyCfg)

		l := logx.As()
		l.Debug().
			Strs("args", args).
			Bool("monitorBlockNode", alloyCfg.MonitorBlockNode).
			Str("clusterName", alloyCfg.ClusterName).
			Str("environment", alloyCfg.Environment).
			Int("prometheusRemotes", len(alloyCfg.PrometheusRemotes)).
			Int("lokiRemotes", len(alloyCfg.LokiRemotes)).
			Msg("Installing Alloy observability stack")

		// Warn about deprecated flags
		if flagPrometheusURL != "" || flagPrometheusUsername != "" || flagLokiURL != "" || flagLokiUsername != "" {
			l.Warn().Msg("--prometheus-url, --prometheus-username, --loki-url, and --loki-username flags are deprecated; please use --add-prometheus-remote and --add-loki-remote instead")
		}

		if alloyCfg.Environment != "" && !alloy.EmitsEnvironmentLabel(alloyCfg) {
			l.Warn().Str("environment", alloyCfg.Environment).
				Msg("environment has no effect: no remote uses labelProfile=ops")
		}

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return err
		}
		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		wb := workflows.WithWorkflowExecutionMode(workflows.NewAlloyInstallWorkflow(), opts)

		intent := models.Intent{Action: models.ActionInstall, Target: models.TargetAlloy}
		inputs := map[string]any{
			"profile": flagProfile, "clusterName": alloyCfg.ClusterName, "environment": alloyCfg.Environment,
			"monitorBlockNode": alloyCfg.MonitorBlockNode,
		}
		if err := common.RunWorkflowRecordingCluster(cmd.Context(), wb, intent, inputs); err != nil {
			return err
		}

		l.Info().Msg("Successfully installed Alloy observability stack")
		return nil
	},
}

func configFileHint() string {
	if f := config.File(); f != "" {
		return "Fix the alloy block in " + f
	}
	return "Fix the alloy block in the --config file"
}
