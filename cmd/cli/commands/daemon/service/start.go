// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the solo-provisioner-daemon systemd service",
	Long:  "Start the solo-provisioner-daemon systemd service via systemctl. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := pkgos.StartService(cmd.Context(), daemonServiceName); err != nil {
			if ex := errorx.Cast(err); ex != nil {
				return ex.WithProperty(models.ErrPropertyResolution, []string{
					"Check service logs: sudo journalctl -u solo-provisioner-daemon -n 50 --no-pager",
					"Check service status: sudo systemctl status solo-provisioner-daemon",
					"Verify daemon binary: ls -la /opt/solo/weaver/bin/solo-provisioner-daemon",
					"Verify daemon config: cat /opt/solo/weaver/config/daemon.yaml",
					"Verify daemon kubeconfig: ls -la /opt/solo/weaver/config/daemon.kubeconfig",
					"If not yet installed: sudo solo-provisioner daemon service install",
				})
			}
			return err
		}
		return nil
	},
}
