// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
)

var flagESOChartVersion string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the External Secrets Operator",
	Long: `Install the External Secrets Operator (ESO) Helm chart into the cluster.

The command is idempotent: when ESO is already installed in the target
namespace, installation is skipped with a clear message.

Examples:
  # Install ESO with defaults (namespace: external-secrets, catalog default version)
  solo-provisioner eso operator install

  # Install into a custom namespace
  solo-provisioner eso operator install --namespace my-eso

  # Pin a specific catalog-declared chart version
  solo-provisioner eso operator install --chart-version 0.20.2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("namespace", flagESONamespace).
			Str("chartVersion", flagESOChartVersion).
			Msg("Installing External Secrets Operator")

		wb, err := workflows.NewESOInstallWorkflow(workflows.ESOInstallOptions{
			Namespace: flagESONamespace,
			Version:   flagESOChartVersion,
		})
		if err != nil {
			return err
		}

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		l.Info().Msg("Successfully installed External Secrets Operator")
		return nil
	},
}

func init() {
	common.FlagESONamespace().SetVar(installCmd, &flagESONamespace, false)
	common.FlagESOChartVersion().SetVar(installCmd, &flagESOChartVersion, false)
}
