// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Teleport node agent",
	Long:  "Uninstall the Teleport node agent, stopping the systemd service and removing binaries and configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Msg("Uninstalling Teleport node agent")

		sm, err := state.NewStateManager()
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to initialise state manager")
		}

		if err = sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
			return errorx.IllegalState.Wrap(err, "failed to refresh state from disk")
		}

		wb := workflows.NewTeleportNodeAgentUninstallWorkflow(sm)

		common.RunWorkflowBuilder(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully uninstalled Teleport node agent")
		return nil
	},
}
