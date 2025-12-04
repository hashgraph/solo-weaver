package commands

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/common"
	"golang.hedera.com/solo-weaver/internal/workflows"
)

var selfInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Perform self-installation of Solo Weaver components",
	Long:  "Perform self-installation of Solo Weaver components on the local system",
	RunE: func(cmd *cobra.Command, args []string) error {
		common.RunWorkflow(cmd.Context(), workflows.NewSelfInstallWorkflow())
		logx.As().Info().Msg("Self-installation of Solo Weaver components completed successfully")
		return nil
	},
}
