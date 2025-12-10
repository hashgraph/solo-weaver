package commands

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
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
