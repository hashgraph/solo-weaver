package kube

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/kube/cluster"
	"github.com/spf13/cobra"
)

var (
	flagProfile string

	kubeCmd = &cobra.Command{
		Use:   "kube",
		Short: "Manage Kubernetes Cluster & its components",
		Long:  "Manage Kubernetes Cluster & its components",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable so that sub-commands would inherit parent flags
	}
)

func init() {
	common.FlagProfile.SetVarP(kubeCmd, &flagProfile, false)
	kubeCmd.AddCommand(cluster.GetCmd())
}

func GetCmd() *cobra.Command {
	return kubeCmd
}
