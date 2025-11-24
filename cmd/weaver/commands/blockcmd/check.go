package blockcmd

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/common"
	"golang.hedera.com/solo-weaver/internal/workflows"
)

var blockNodeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness for Hedera Block node",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera Block node",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Str("args", fmt.Sprintf("%v", args)).
			Msg("Running solo weaver preflight checks for block node")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodePreflightCheckWorkflow(flagProfile))

		logx.As().Info().Msg("Node preflight checks completed successfully for block node")
		return nil
	},
}
