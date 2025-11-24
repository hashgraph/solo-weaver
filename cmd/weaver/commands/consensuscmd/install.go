package consensuscmd

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var consensusNodeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Hedera Consensus Node",
	Long:  "Run safety checks, setup a K8s cluster and install a Hedera Consensus Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errorx.NotImplemented.New("consensus node installation is not yet implemented")
	},
}
