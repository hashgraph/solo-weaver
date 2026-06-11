// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the solo-provisioner-daemon systemd service",
	Long:  "Start the solo-provisioner-daemon systemd service via systemctl, then verify it is healthy. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewDaemonServiceStartWorkflow()); err != nil {
			return err
		}

		paths := models.Paths()
		if warning := steps.CheckDaemonComponentPrerequisites(paths.DaemonSockPath); warning != "" {
			logx.As().Warn().Msg(warning)
			return errorx.IllegalState.New(
				"daemon started but component prerequisites are not satisfied — " +
					"fix the issues listed above and re-run: solo-provisioner daemon service check")
		}

		logx.As().Info().Msg("solo-provisioner-daemon service started and healthy")
		return nil
	},
}
