// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"strconv"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/kube"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
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
	Short: "Create the `inet weaver-host-firewall` table (create-if-missing; --force re-renders)",
	Long: "Render and apply the full `inet weaver-host-firewall` table, either from flags or from a declarative " +
		"config file (--from-file). create-if-missing: if the table already exists, no changes are made unless " +
		"--force is passed, which re-renders from the current flags or file.\n\n" +
		"--from-file states the whole table at once; `create-allow-rule` declares a single named allow rule without " +
		"a file. --from-file is fully declarative: the file states the whole " +
		"table, nothing is inherited from the host's current firewall, and an allow rule absent from the file is " +
		"removed. The three reserved blocks (mgmt, blocked, in_cluster) are therefore required, as is `cidrs` inside " +
		"mgmt and blocked — a block left out would fall back to a weaver default the file never stated, which for " +
		"mgmt is an empty allowlist under the default-drop policy. To disable a reserved block, give it an empty " +
		"address list (`in_cluster: {cidrs: []}`); omitting `in_cluster.cidrs` instead means \"auto-detect this " +
		"node's pod CIDR\".",
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := buildTable(cmd)
		if err != nil {
			return err
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}

		changed, err := newManager().Create(cmd.Context(), t, force)
		if err != nil {
			return err
		}

		// Record the enable decision even when the create was a no-op (the table
		// already existed and --force was not passed): "this host wants a host
		// firewall" is true either way, and it is the decision — not the ruleset —
		// that a later `block node reconfigure` seeds its enable/disable choice from.
		recordHostFirewallDecision(false)

		if changed {
			logx.As().Info().Msg("inet weaver-host-firewall firewall is in the desired state")
		}
		return nil
	},
}

// buildTable assembles the desired table from --from-file or from the individual
// flags.
func buildTable(cmd *cobra.Command) (*fw.Table, error) {
	if cmd.Flags().Changed("from-file") {
		cfg, err := fw.LoadConfigFile(flagFromFile)
		if err != nil {
			return nil, err
		}
		t, err := cfg.Table()
		if err != nil {
			return nil, err
		}
		// An omitted in-cluster block means "use the cluster's pod CIDR"; an
		// explicitly empty list means "render no in-cluster rule". Only the former
		// triggers detection.
		if cfg.InClusterCIDRsUnset() {
			applyDetectedPodCIDR(cmd, t)
		}
		return t, nil
	}

	// NewTable() seeds the design defaults (SSH 22, the stack in-cluster port
	// set). Override a field only when its flag was explicitly set: the
	// flag-binding vars are shared across verbs (see firewall.go), so a later
	// verb's registration clobbers another verb's default in the shared
	// variable. Reading the shared value unconditionally would wipe
	// --in-cluster-ports to nil on a plain `create --force`; gating on Changed()
	// keeps NewTable()'s default authoritative.
	t := fw.NewTable()
	if cmd.Flags().Changed("mgmt-cidrs") {
		t.Mgmt.CIDRs = flagMgmtCIDRs
	}
	if cmd.Flags().Changed("blocked-cidrs") {
		t.Blocked.CIDRs = flagBlockedCIDRs
	}
	if cmd.Flags().Changed("in-cluster-ports") {
		t.InCluster.Ports = fw.PortStrings(flagInClusterPorts)
	}
	if cmd.Flags().Changed("ssh-port") {
		t.Mgmt.Ports = []string{strconv.Itoa(flagSSHPort)}
	}

	// --pod-cidr accepts a mixed v4/v6 list; the renderer routes each entry to
	// its family's set, so no slotting is needed here.
	//
	// When the operator passes nothing, auto-detection resolves the local node's
	// .spec.podCIDR. Detection is best-effort — `network firewall create` is
	// node-agnostic and may run before a cluster exists — so if no cluster is
	// reachable we fall back to omitting the in-cluster rule and tell the
	// operator how to set it.
	if len(flagPodCIDR) > 0 {
		t.InCluster.CIDRs = flagPodCIDR
	} else {
		applyDetectedPodCIDR(cmd, t)
	}

	return t, nil
}

func applyDetectedPodCIDR(cmd *cobra.Command, t *fw.Table) {
	cidr, err := detectPodCIDR(cmd.Context())
	if err != nil {
		logx.As().Warn().Err(err).Msg(
			"could not auto-detect pod CIDR; the in-cluster host-service ports rule will be omitted — pass --pod-cidr to set it explicitly")
		return
	}
	t.InCluster.CIDRs = []string{cidr}
	logx.As().Info().Str("pod_cidr", cidr).Msg("auto-detected pod CIDR from the local node")
}

func init() {
	createCmd.Flags().StringSliceVar(&flagMgmtCIDRs, "mgmt-cidrs", nil, "Management/SSH allowlist CIDRs (comma-separated or repeated)")
	createCmd.Flags().StringSliceVar(&flagBlockedCIDRs, "blocked-cidrs", nil, "Operator-curated block list CIDRs, dropped before any other rule (comma-separated or repeated)")
	createCmd.Flags().IntSliceVar(&flagInClusterPorts, "in-cluster-ports", fw.DefaultInClusterPorts, "Host-service ports reachable from the pod CIDR")
	createCmd.Flags().IntVar(&flagSSHPort, "ssh-port", fw.DefaultSSHPort, "SSH/management TCP port accepted from the allowlist")
	createCmd.Flags().StringSliceVar(&flagPodCIDR, "pod-cidr", nil, "Pod CIDR(s) allowed to reach the in-cluster host-service ports; may be IPv4 and/or IPv6 (comma-separated or repeated). Default: auto-detected from the local node's .spec.podCIDR; the rule is omitted if no cluster is reachable")
	createCmd.Flags().StringVar(&flagFromFile, "from-file", "", "Declarative YAML config to render the whole table from; mutually exclusive with the individual flags")

	// A file states the whole table, so mixing it with a flag that states part of
	// one would leave the precedence between them to guesswork.
	createCmd.MarkFlagsMutuallyExclusive("from-file", "mgmt-cidrs")
	createCmd.MarkFlagsMutuallyExclusive("from-file", "blocked-cidrs")
	createCmd.MarkFlagsMutuallyExclusive("from-file", "in-cluster-ports")
	createCmd.MarkFlagsMutuallyExclusive("from-file", "ssh-port")
	createCmd.MarkFlagsMutuallyExclusive("from-file", "pod-cidr")
}
