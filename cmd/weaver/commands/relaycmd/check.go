package relaycmd

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var relayNodeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness for Hedera relay node",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera relay node components",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errorx.NotImplemented.New("relay node preflight checks are not yet implemented")
	},
}
