// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the External Secrets Operator",
	Long: `Install the External Secrets Operator (ESO) Helm chart into the cluster.

The command is idempotent: when ESO is already installed in the target
namespace, installation is skipped with a clear message. The chart version is
pinned by the infrastructure catalog.

Examples:
  # Install ESO with defaults (namespace: external-secrets)
  solo-provisioner eso operator install

  # Install into a custom namespace
  solo-provisioner eso operator install --namespace my-eso`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("namespace", flagESONamespace).
			Msg("Installing External Secrets Operator")

		wb := workflows.NewESOInstallWorkflow(flagESONamespace)

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		l.Info().Msg("Successfully installed External Secrets Operator")
		return nil
	},
}

func init() {
	common.FlagESONamespace().SetVar(installCmd, &flagESONamespace, false)
}
