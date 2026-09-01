// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

// Shared by create and delete, which both address an ExternalSecret by name.
var (
	flagSecretName      string
	flagSecretNamespace string
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage External Secrets Operator secrets",
	Long:  "Manage ExternalSecret resources that the External Secrets Operator (ESO) reconciles into Kubernetes Secrets.",
	RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
}

func init() {
	secretCmd.AddCommand(createCmd)
	secretCmd.AddCommand(deleteCmd)
}

func GetCmd() *cobra.Command {
	return secretCmd
}
