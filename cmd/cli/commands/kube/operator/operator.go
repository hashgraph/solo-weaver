// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var (
	// flagImagePullSecret names a docker-registry Secret in the operator namespace
	// used to pull the operator's private images. It must already exist (this command
	// does not create it). Empty installs with no pull secret (public images only).
	flagImagePullSecret string

	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	operatorCmd = &cobra.Command{
		Use:   "operator",
		Short: "Manage the solo-operator lifecycle",
		Long: "Install and uninstall the solo-operator (and its CRDs) on the cluster. " +
			"Separate from 'kube cluster install' because the operator's chart and images " +
			"may live in a private registry that needs credentials set up first.",
		RunE: common.DefaultRunE,
	}
)

func init() {
	operatorCmd.AddCommand(installCmd)
	operatorCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return operatorCmd
}
