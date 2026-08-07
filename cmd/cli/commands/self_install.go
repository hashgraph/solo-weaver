// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var selfInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Perform self-installation of Solo Provisioner",
	Long:  "Perform self-installation of Solo Provisioner on the local system",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewSelfInstallWorkflow()); err != nil {
			return err
		}

		// RunPersistentPreRun (and therefore RunStartupMigrations) is skipped for
		// the install command, so we invoke it explicitly here. This ensures
		// startup-scoped migrations such as LegacyBinaryMigration run even when
		// the user is installing or re-installing.
		if err := common.RunStartupMigrations(cmd.Context()); err != nil {
			return err
		}

		logx.As().Info().Msg("Solo Provisioner is installed successfully; run 'solo-provisioner -h' to see available commands")
		return nil
	},
}

// flagYes gates self-uninstall. Uninstall removes far more than the CLI binary —
// the daemon and its service, the network boot units, and the config tree under
// /etc — and none of it is recoverable, so the operator has to ask for it
// explicitly.
//
// Deliberately no -y short form: the whole point is an act of confirmation, and
// -y is the flag operators append by reflex.
var flagYes bool

var selfUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Solo Provisioner from the local system",
	Long: "Uninstall Solo Provisioner from the local system.\n\n" +
		"Requires --yes. This removes the solo-provisioner CLI, the\n" +
		"solo-provisioner-daemon binary and its systemd service, the network boot\n" +
		"units, and the configuration tree under /etc/solo-provisioner. Tear down\n" +
		"the block node and the cluster first.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !flagYes {
			return errorx.IllegalArgument.New("refusing to uninstall without --yes").
				WithProperty(models.ErrPropertyResolution, []string{
					"Uninstall removes, irreversibly:",
					"  - the solo-provisioner CLI and its /usr/local/bin symlink",
					"  - the solo-provisioner-daemon binary and its systemd service",
					"  - the network-nft and bandwidth-shaper boot units",
					"  - the configuration tree under /etc/solo-provisioner",
					"Re-run to confirm: sudo solo-provisioner uninstall --yes",
				})
		}
		return common.RunWorkflowBuilder(cmd.Context(), workflows.NewSelfUninstallWorkflow())
	},
}

func init() {
	selfUninstallCmd.Flags().BoolVar(&flagYes, "yes", false,
		"Confirm removal of the CLI, the daemon and its service, the network boot units, and /etc/solo-provisioner")
}
