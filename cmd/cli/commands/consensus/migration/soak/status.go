// SPDX-License-Identifier: Apache-2.0

package soak

import (
	"encoding/json"
	"fmt"

	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

const daemonServiceName = "solo-provisioner-daemon"

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current consensus-node migration soak status",
	Long:  "Fetch and display the soak watcher state from solo-provisioner-daemon.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sockPath := models.Paths().DaemonSockPath
		status := steps.SoakStatus(sockPath)
		if status == nil {
			return errorx.IllegalState.New(
				"could not reach daemon at %s", sockPath).
				WithProperty(models.ErrPropertyResolution, []string{
					fmt.Sprintf("Verify daemon is running: sudo systemctl status %s", daemonServiceName),
					fmt.Sprintf("Check daemon journal: sudo journalctl -u %s -n 20 --no-pager", daemonServiceName),
					"If not installed: sudo solo-provisioner daemon service install",
				})
		}

		out, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return errorx.IllegalState.New("marshal soak status: %v", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}
