// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/kube/cluster"
	"github.com/spf13/cobra"
)

var (
	// flagProfile backs the deprecated --profile flag. Cluster install is now
	// workload-agnostic (it validates only the Kubernetes substrate floor), so the
	// value is ignored. The flag is kept hidden for backward compatibility so that
	// existing invocations/scripts do not break; see install.go for the ignore notice.
	flagProfile string

	kubeCmd = &cobra.Command{
		Use:   "kube",
		Short: "Manage Kubernetes Cluster & its components",
		Long:  "Manage Kubernetes Cluster & its components",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable so that sub-commands would inherit parent flags
	}
)

func init() {
	// Deprecated: --profile no longer affects cluster install (substrate-only floor).
	// Kept hidden + persistent for backward compatibility; per-workload sizing lives
	// on the block node commands.
	common.FlagProfile().SetVarPHidden(kubeCmd, &flagProfile, false)
	kubeCmd.AddCommand(cluster.GetCmd())
}

func GetCmd() *cobra.Command {
	return kubeCmd
}
