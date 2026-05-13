// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run Solo Provisioner as a system daemon",
	Long:  "Long-lived foreground process started by the solo-provisioner.service systemd unit.",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Info().Msg("Solo Provisioner daemon started")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

		select {
		case sig := <-sigChan:
			logx.As().Info().Str("signal", sig.String()).Msg("Solo Provisioner daemon shutting down")
		case <-cmd.Context().Done():
			logx.As().Info().Msg("Solo Provisioner daemon context cancelled")
		}

		return nil
	},
}

func init() {
	// Skip the global weaver-installation check — the daemon itself IS the
	// installed process; running the check would deadlock on a fresh host.
	common.SkipGlobalChecks(daemonCmd)
}
