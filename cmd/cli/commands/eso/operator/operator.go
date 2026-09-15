// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagESONamespace string

	// Error-control flags (persistent → apply to both install and uninstall)
	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "Manage the External Secrets Operator installation",
	Long:  "Install and manage the External Secrets Operator (ESO) Helm chart in the cluster.",
	RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
}

func init() {
	// Persistent on the operator command, so both install and uninstall inherit
	// them — mirrors the alloy cluster pattern in alloy/cluster/cluster.go.
	common.FlagESONamespace().SetVarP(operatorCmd, &flagESONamespace, false)
	common.FlagStopOnError().SetVarP(operatorCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(operatorCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(operatorCmd, &flagContinueOnError, false)
	operatorCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)

	operatorCmd.AddCommand(installCmd)
	operatorCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return operatorCmd
}
