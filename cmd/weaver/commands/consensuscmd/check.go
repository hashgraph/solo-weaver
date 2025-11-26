package consensuscmd

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var consensusNodeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness for Hedera Consensus node",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera Consensus node components",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errorx.NotImplemented.New("consensus node preflight checks are not yet implemented")
	},
}
