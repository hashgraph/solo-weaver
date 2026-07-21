// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

var (
	flagTCAttachVeth   string
	flagTCAttachDetach bool

	tcAttachCmd = &cobra.Command{
		Use:   "tc-attach",
		Short: "Install or tear down the ingress traffic-shaper HTB hierarchy on a block node pod's host-side veth",
		Long: "Install the $VETH ingress HTB hierarchy (classes 1:10/1:20/1:30, default reserve-ingress, " +
			"fq_codel leaves, no tc filters) on the given host-side veth, using the per-class ingress " +
			"budgets recorded by `network shape`.\n\n" +
			"This is a privileged worker (root). The solo-provisioner-daemon's pod-lifecycle watcher execs " +
			"it via sudo on each block node pod create, passing the veth it resolved for the pod; an " +
			"operator can also run it by hand for debugging. With --detach it tears the hierarchy down " +
			"instead (best-effort — the kernel auto-removes veth-attached qdiscs when the pod's veth " +
			"disappears).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flagTCAttachVeth == "" {
				return errorx.IllegalArgument.New("--veth is required")
			}

			m := shape.NewManager()
			if flagTCAttachDetach {
				if err := m.RemoveIngressVeth(cmd.Context(), flagTCAttachVeth); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "detached ingress HTB from %s\n", flagTCAttachVeth)
				return nil
			}
			if err := m.ApplyIngressVeth(cmd.Context(), flagTCAttachVeth); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "attached ingress HTB to %s\n", flagTCAttachVeth)
			return nil
		},
	}
)

func init() {
	// Narrow privileged worker (the daemon watcher execs it via sudo, or an
	// operator runs it by hand): it only touches the recorded shape config and
	// the live veth qdisc, so it needs neither the weaver-installation check nor
	// startup migrations (both read root-owned state via RunPersistentPreRun).
	// root.go's superuser gate still independently requires root — unlike
	// reconcile-shaper, tc-attach has no unprivileged --check path.
	common.SkipGlobalChecks(tcAttachCmd)

	tcAttachCmd.Flags().StringVar(&flagTCAttachVeth, "veth", "",
		"Host-side veth interface to (de)install the ingress HTB hierarchy on, e.g. lxc1a2b3c (required)")
	tcAttachCmd.Flags().BoolVar(&flagTCAttachDetach, "detach", false,
		"Tear down the ingress HTB hierarchy on the veth instead of installing it (best-effort)")
	_ = tcAttachCmd.MarkFlagRequired("veth")
}
