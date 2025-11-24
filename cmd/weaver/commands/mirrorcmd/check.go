package mirrorcmd

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var mirrorNodeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness for Hedera mirror node",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera mirror node components",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errorx.NotImplemented.New("mirror node preflight checks are not yet implemented")
	},
}
