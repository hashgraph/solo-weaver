package mirrorcmd

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var mirrorNodeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Hedera mirror Node",
	Long:  "Run safety checks, setup a K8s cluster and install a Hedera mirror Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errorx.NotImplemented.New("mirror node installation is not yet implemented")
	},
}
