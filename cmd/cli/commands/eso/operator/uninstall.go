// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
)

var flagESOUninstallNamespace string

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the External Secrets Operator",
	Long: `Uninstall the External Secrets Operator (ESO) Helm release from the cluster.

The command is idempotent: when ESO is not installed in the target namespace, it
exits cleanly with a skip message.

Warning: uninstalling ESO removes its cluster-scoped CRDs, which deletes every
ExternalSecret and SecretStore resource in the cluster (and the Kubernetes Secrets
they sync). Do not run this while other components still rely on synced secrets.

Examples:
  # Uninstall ESO from the default namespace (external-secrets)
  solo-provisioner eso operator uninstall

  # Uninstall from a custom namespace
  solo-provisioner eso operator uninstall --namespace my-eso`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("namespace", flagESOUninstallNamespace).
			Msg("Uninstalling External Secrets Operator")

		wb := workflows.NewESOUninstallWorkflow(flagESOUninstallNamespace)

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		l.Info().Msg("Successfully uninstalled External Secrets Operator")
		return nil
	},
}

func init() {
	common.FlagESOUninstallNamespace().SetVar(uninstallCmd, &flagESOUninstallNamespace, false)
}
