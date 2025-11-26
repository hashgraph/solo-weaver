package relaycmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var relayNodeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Hedera relay Node",
	Long:  "Run safety checks, setup a K8s cluster and install a Hedera relay Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("relay node installation is not yet implemented")
	},
}
