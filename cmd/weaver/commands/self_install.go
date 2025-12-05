package commands

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/common"
	"golang.hedera.com/solo-weaver/internal/workflows"
)

var selfInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Perform self-installation of Solo Weaver",
	Long:  "Perform self-installation of Solo Weaver on the local system",
	RunE: func(cmd *cobra.Command, args []string) error {
		common.RunWorkflow(cmd.Context(), workflows.NewSelfInstallWorkflow())
		logx.As().Info().Msg("Solo Weaver is installed successfully; run 'weaver -h' to see available commands")
		return nil
	},
}
