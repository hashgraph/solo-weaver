// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/kube/cluster"
	"github.com/spf13/cobra"
)

var (
	// flagProfile backs the optional --profile flag. On cluster install it sizes the
	// preflight hardware floor to the intended workload (see install.go); omitted, the
	// substrate-only floor is used.
	flagProfile string

	kubeCmd = &cobra.Command{
		Use:   "kube",
		Short: "Manage Kubernetes Cluster & its components",
		Long:  "Manage Kubernetes Cluster & its components",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable so that sub-commands would inherit parent flags
	}
)

func init() {
	// --profile optionally sizes the cluster-install preflight hardware floor to the
	// intended workload. Persistent so cluster subcommands inherit it.
	common.FlagProfile().SetVarP(kubeCmd, &flagProfile, false)
	kubeCmd.AddCommand(cluster.GetCmd())
}

func GetCmd() *cobra.Command {
	return kubeCmd
}
