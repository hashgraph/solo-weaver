// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the solo-provisioner-daemon systemd service",
	Long:  "Stop the solo-provisioner-daemon systemd service via systemctl. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := pkgos.StopService(cmd.Context(), daemonServiceName); err != nil {
			if ex := errorx.Cast(err); ex != nil {
				return ex.WithProperty(models.ErrPropertyResolution, []string{
					"Check service status: sudo systemctl status solo-provisioner-daemon",
					"Check service logs: sudo journalctl -u solo-provisioner-daemon -n 50 --no-pager",
					"Force-kill if stuck: sudo systemctl kill solo-provisioner-daemon",
				})
			}
			return err
		}
		return nil
	},
}
