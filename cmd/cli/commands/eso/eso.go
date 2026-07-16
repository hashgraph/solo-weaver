// SPDX-License-Identifier: Apache-2.0

package eso

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/eso/operator"
	"github.com/spf13/cobra"
)

var esoCmd = &cobra.Command{
	Use:   "eso",
	Short: "Manage External Secrets Operator (ESO)",
	Long:  "Manage the External Secrets Operator (ESO) used to sync secrets from external stores into Kubernetes.",
	RunE:  common.DefaultRunE, // provide a default action so the parent command is runnable and its PersistentPreRunE hooks are invoked; persistent flags are inherited by subcommands regardless
}

func init() {
	esoCmd.AddCommand(operator.GetCmd())
}

func GetCmd() *cobra.Command {
	return esoCmd
}
