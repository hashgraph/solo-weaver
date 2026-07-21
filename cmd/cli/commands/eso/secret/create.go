// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"time"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

var (
	flagSecretStore           string
	flagSecretName            string
	flagSecretNamespace       string
	flagSecretRefreshInterval string
	flagSecretSet             []string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create or update an ExternalSecret",
	Long: `Create (or update) an ExternalSecret resource that the External Secrets
Operator reconciles into a Kubernetes Secret.

The command uses server-side apply, so re-running it with the same --name and
--namespace updates the existing ExternalSecret in place.

A ClusterSecretStore named by --store and the target --namespace must both
already exist in the cluster.

Example:
  solo-provisioner eso secret create \
    --store=vault-store \
    --name=grafana-alloy-secrets \
    --namespace=grafana-alloy \
    --set PROMETHEUS_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/prometheus/primary#password \
    --set LOKI_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/loki/primary#password`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := sanity.ValidateIdentifier(flagSecretStore); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --store")
		}
		if err := sanity.ValidateIdentifier(flagSecretName); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --name")
		}
		if err := sanity.ValidateIdentifier(flagSecretNamespace); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --namespace")
		}
		refreshInterval, err := normalizeRefreshInterval(flagSecretRefreshInterval)
		if err != nil {
			return err
		}

		entries, err := parseSetFlags(flagSecretSet)
		if err != nil {
			return err
		}

		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("store", flagSecretStore).
			Str("name", flagSecretName).
			Str("namespace", flagSecretNamespace).
			Int("entries", len(entries)).
			Msg("Creating ExternalSecret")

		wb := workflows.NewESOSecretCreateWorkflow(workflows.ESOSecretOptions{
			Name:            flagSecretName,
			Namespace:       flagSecretNamespace,
			StoreName:       flagSecretStore,
			RefreshInterval: refreshInterval,
			Data:            entries,
		})

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		l.Info().Msg("Successfully created ExternalSecret")
		return nil
	},
}

// normalizeRefreshInterval validates the --refresh-interval value as a
// non-negative Go duration and returns it in the canonical form ESO stores
// (e.g. 1h -> 1h0m0s), so identical re-runs stay server-side-apply no-ops.
// It returns an IllegalArgument error on malformed or negative input.
func normalizeRefreshInterval(s string) (string, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return "", errorx.IllegalArgument.Wrap(err, "invalid --refresh-interval %q, expected a Go duration (e.g. 1h, 30m)", s)
	}
	if d < 0 {
		return "", errorx.IllegalArgument.New("invalid --refresh-interval %q, duration must not be negative", s)
	}
	return d.String(), nil
}

func init() {
	common.FlagESOSecretStore().SetVar(createCmd, &flagSecretStore, true)
	common.FlagESOSecretName().SetVar(createCmd, &flagSecretName, true)
	common.FlagESOSecretNamespace().SetVar(createCmd, &flagSecretNamespace, true)
	common.FlagESORefreshInterval().SetVar(createCmd, &flagSecretRefreshInterval, false)
	// --set required-ness is enforced by parseSetFlags (at least one entry).
	common.FlagESOSecretSet().SetVar(createCmd, &flagSecretSet, false)
}
