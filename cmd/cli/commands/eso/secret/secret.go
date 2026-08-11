// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage External Secrets Operator secrets",
	Long:  "Manage ExternalSecret resources that the External Secrets Operator (ESO) reconciles into Kubernetes Secrets.",
	RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
}

func init() {
	secretCmd.AddCommand(createCmd)
}

func GetCmd() *cobra.Command {
	return secretCmd
}
