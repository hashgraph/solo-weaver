// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/kube"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/spf13/cobra"
)

// detectPodCIDR resolves the local node's pod CIDR from the cluster. It is
// indirected through a var so command tests can stub cluster access.
var detectPodCIDR = func(ctx context.Context) (string, error) {
	c, err := kube.NewClient()
	if err != nil {
		return "", err
	}
	return c.DetectNodePodCIDR(ctx)
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create the `inet host` table (create-if-missing; --force re-renders)",
	Long: "Render and apply the full `inet host` table. create-if-missing: if the table already " +
		"exists, no changes are made unless --force is passed, which re-renders from the flags.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// NewTable() seeds the design defaults (SSH 22, the stack in-cluster
		// port set). Override a field only when its flag was explicitly set:
		// the flag-binding vars are shared across verbs (see firewall.go), so a
		// later verb's registration clobbers another verb's default in the
		// shared variable. Reading the shared value unconditionally would wipe
		// --in-cluster-ports to nil on a plain `create --force`; gating on
		// Changed() keeps NewTable()'s default authoritative.
		t := fw.NewTable()
		if cmd.Flags().Changed("mgmt-cidrs") {
			t.MgmtCIDRs = flagMgmtCIDRs
		}
		if cmd.Flags().Changed("blocked-cidrs") {
			t.BlockedCIDRs = flagBlockedCIDRs
		}
		if cmd.Flags().Changed("in-cluster-ports") {
			t.InClusterPorts = flagInClusterPorts
		}
		if cmd.Flags().Changed("ssh-port") {
			t.SSHPort = flagSSHPort
		}

		// --pod-cidr accepts a mixed v4/v6 list; route each entry to the matching
		// family slot so a dual-stack node can admit in-cluster traffic over both.
		// A value that fails family classification is slotted as v4 so Table.Validate
		// surfaces a clear --pod-cidr error rather than dropping it silently.
		//
		// When the operator passes nothing, auto-detection resolves the local
		// node's .spec.podCIDR (a single, v4 value today). Detection is
		// best-effort — `network firewall create` is node-agnostic and may run
		// before a cluster exists, so if no cluster is reachable we fall back to
		// omitting the in-cluster-ports rule and tell the operator how to set it.
		if len(flagPodCIDR) > 0 {
			for _, c := range flagPodCIDR {
				if isV6, err := sanity.CIDRIsIPv6(c); err == nil && isV6 {
					t.PodCIDR6 = c
				} else {
					t.PodCIDR = c
				}
			}
		} else {
			if cidr, err := detectPodCIDR(cmd.Context()); err != nil {
				logx.As().Warn().Err(err).Msg(
					"could not auto-detect pod CIDR; the in-cluster host-service ports rule will be omitted — pass --pod-cidr to set it explicitly")
			} else {
				t.PodCIDR = cidr
				logx.As().Info().Str("pod_cidr", cidr).Msg("auto-detected pod CIDR from the local node")
			}
		}

		if len(t.MgmtCIDRs) == 0 {
			logx.As().Warn().Msg(
				"no --mgmt-cidrs set: the SSH allow rule will match no sources under the default-drop policy — " +
					"you will be locked out of new SSH connections; pass --mgmt-cidrs to set the management allowlist")
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}

		changed, err := newManager().Create(cmd.Context(), t, force)
		if err != nil {
			return err
		}
		if changed {
			logx.As().Info().Msg("inet host firewall is in the desired state")
		}
		return nil
	},
}

func init() {
	createCmd.Flags().StringSliceVar(&flagMgmtCIDRs, "mgmt-cidrs", nil, "Management/SSH allowlist CIDRs (comma-separated or repeated)")
	createCmd.Flags().StringSliceVar(&flagBlockedCIDRs, "blocked-cidrs", nil, "Operator-curated block list CIDRs, dropped before any other rule (comma-separated or repeated)")
	createCmd.Flags().IntSliceVar(&flagInClusterPorts, "in-cluster-ports", fw.DefaultInClusterPorts, "Host-service ports reachable from the pod CIDR")
	createCmd.Flags().IntVar(&flagSSHPort, "ssh-port", fw.DefaultSSHPort, "SSH/management TCP port accepted from the allowlist")
	createCmd.Flags().StringSliceVar(&flagPodCIDR, "pod-cidr", nil, "Pod CIDR(s) allowed to reach the in-cluster host-service ports; may be IPv4 and/or IPv6 (comma-separated or repeated). Default: auto-detected from the local node's .spec.podCIDR; the rule is omitted if no cluster is reachable")
}
