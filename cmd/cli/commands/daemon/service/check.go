// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:     "check",
	Aliases: []string{"status"},
	Short:   "Check the health of the solo-provisioner-daemon service",
	Long: "Verify the daemon installation: unit file present, service enabled and running, " +
		"binary exists, sudoers entry in place, Unix socket responding to /health, and all " +
		"component prerequisites satisfied. Exits non-zero if the daemon is running but " +
		"component prerequisites (e.g. upgrade directory ownership) are not yet met.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewDaemonServiceCheckWorkflow()); err != nil {
			return err
		}

		paths := models.Paths()

		// Print the full /status response so the operator can see component state.
		if status := steps.FetchDaemonStatus(paths.DaemonSockPath); status != nil {
			if out, err := json.MarshalIndent(status, "", "  "); err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
			}
		}

		// Exit non-zero if any component probe or monitor is unhealthy.
		if warning := steps.CheckDaemonComponentPrerequisites(paths.DaemonSockPath); warning != "" {
			logx.As().Warn().Msg(warning)
			return errorx.IllegalState.New(
				"daemon is running but component health issues detected — " +
					"fix the issues listed above and re-run: solo-provisioner daemon service check")
		}

		logx.As().Info().Msg("solo-provisioner-daemon service is healthy")
		return nil
	},
}
