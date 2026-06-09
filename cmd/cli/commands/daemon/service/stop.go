// SPDX-License-Identifier: Apache-2.0

package service

import (
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the solo-provisioner-daemon systemd service",
	Long:  "Stop the solo-provisioner-daemon systemd service via systemctl. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return pkgos.StopService(cmd.Context(), daemonServiceName)
	},
}
