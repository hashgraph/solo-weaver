// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var flagESONamespace string

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "Manage the External Secrets Operator installation",
	Long:  "Install and manage the External Secrets Operator (ESO) Helm chart in the cluster.",
	RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
}

func init() {
	operatorCmd.AddCommand(installCmd)
	operatorCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return operatorCmd
}
