// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/kube"
	pol "github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/joomcode/errorx"
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
	Short: "Create a policy: render its rule(s) into the `inet weaver` chain",
	Long: "Render one named category's classification/ACL rule(s) into the `inet weaver` forward chain and " +
		"ensure its nft set exists. Specify exactly one action: --stamp <class> (classify into an HTB priority " +
		"class), or --deny (drop the CIDRs both directions). There is no --direction flag: --stamp's class fixes " +
		"the direction. --reply-stamp adds an asymmetric conntrack reply rule to an egress-direction --stamp " +
		"policy; the reply class must be the mirror (ingress) direction. create-if-missing: an existing policy " +
		"is left untouched unless --force is passed, which replaces its config and membership from the given " +
		"flags/--cidrs.",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := policyFromFlags(cmd)
		if err != nil {
			return err
		}

		cidrs, err := resolveCIDRs(cmd)
		if err != nil {
			return err
		}

		podCIDR := flagPodCIDR
		if podCIDR == "" && p.Action != pol.ActionDeny {
			// A --deny rule never references POD_CIDR (see Render), so skip
			// resolving it entirely for --deny -- no point requiring a
			// reachable cluster (or --pod-cidr) for a quarantine-only policy.
			// If a sibling --stamp policy in the registry still needs it,
			// Manager.Create's Render call below surfaces that clearly.
			//
			// An explicit --pod-cidr works offline; only auto-detection needs a
			// reachable cluster. Unlike `network firewall create` (best-effort,
			// node-agnostic), `network policy create` is expected to run against a
			// live cluster (design §7.2.4), so a detection failure with no
			// --pod-cidr is a hard error rather than a warning.
			detected, derr := detectPodCIDR(cmd.Context())
			if derr != nil {
				return errorx.IllegalState.Wrap(derr,
					"could not auto-detect the pod CIDR; pass --pod-cidr explicitly")
			}
			podCIDR = detected
			logx.As().Info().Str("pod_cidr", podCIDR).Msg("auto-detected pod CIDR from the local node")
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}

		changed, err := newManager().Create(cmd.Context(), p, cidrs, podCIDR, force)
		if err != nil {
			return err
		}
		if changed {
			logx.As().Info().Str("policy", p.Name).Msg("network policy created")
		}
		return nil
	},
}

// policyFromFlags assembles a Policy from the create flags, resolving the action
// (exactly one of --stamp / --deny) and --from-entity. Deeper validity checks
// (class references, flag combinations) are enforced by Policy.Validate.
func policyFromFlags(cmd *cobra.Command) (*pol.Policy, error) {
	if flagDeny && flagStamp != "" {
		return nil, errorx.IllegalArgument.New("--deny is mutually exclusive with --stamp")
	}

	p := &pol.Policy{
		Name:       flagName,
		Stamp:      flagStamp,
		ReplyStamp: flagReplyStamp,
		Ports:      flagPorts,
	}
	switch {
	case flagDeny:
		p.Action = pol.ActionDeny
	case flagStamp != "":
		p.Action = pol.ActionStamp
	default:
		return nil, errorx.IllegalArgument.New("policy must specify exactly one of --stamp or --deny")
	}

	if flagFromEntity != "" {
		if flagFromEntity != "world" {
			return nil, errorx.IllegalArgument.New("--from-entity accepts only \"world\", got %q", flagFromEntity)
		}
		p.FromEntityWorld = true
	}
	return p, nil
}

// resolveCIDRs returns the initial set membership from --cidrs or --cidrs-file
// (mutually exclusive). Either may be omitted (the set is created empty).
func resolveCIDRs(cmd *cobra.Command) ([]string, error) {
	if cmd.Flags().Changed("cidrs") && cmd.Flags().Changed("cidrs-file") {
		return nil, errorx.IllegalArgument.New("--cidrs and --cidrs-file are mutually exclusive")
	}
	if flagCIDRsFile != "" {
		return readCIDRsFile(flagCIDRsFile)
	}
	return flagCIDRs, nil
}

// readCIDRsFile reads a newline- and/or comma-separated CIDR list from a file,
// skipping blank lines and `#` comments. Per-entry syntax is validated
// downstream by Policy.Validate.
func readCIDRsFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to read --cidrs-file %s", path)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, tok := range strings.Split(line, ",") {
			if v := strings.TrimSpace(tok); v != "" {
				out = append(out, v)
			}
		}
	}
	return out, nil
}

func init() {
	createCmd.Flags().StringVar(&flagName, "name", "", "Policy name; also the nft set name `@<name>` (required)")
	createCmd.Flags().StringVar(&flagStamp, "stamp", "", "HTB class to classify matching packets into; also fixes the policy's direction (mutually exclusive with --deny)")
	createCmd.Flags().BoolVar(&flagDeny, "deny", false, "Drop the --cidrs both directions (mutually exclusive with --stamp)")
	createCmd.Flags().StringVar(&flagReplyStamp, "reply-stamp", "", "Reply class for an asymmetric conntrack reply (requires --stamp to resolve to an egress class; --reply-stamp must resolve to the mirror ingress class)")
	createCmd.Flags().StringVar(&flagFromEntity, "from-entity", "", "Match any source/dest with no IP-set clause (only value: world; mutually exclusive with --cidrs)")
	createCmd.Flags().StringSliceVar(&flagPorts, "ports", nil, "Workload listener ports for the match key (comma-separated or repeated)")
	createCmd.Flags().StringSliceVar(&flagCIDRs, "cidrs", nil, "Initial set membership (comma-separated or repeated); ip:port entries for --reply-stamp")
	createCmd.Flags().StringVar(&flagCIDRsFile, "cidrs-file", "", "Alternative to --cidrs: a file of CIDRs (one per line or comma-separated)")
	createCmd.Flags().StringVar(&flagPodCIDR, "pod-cidr", "", "Pod CIDR to scope classification to (default: auto-detected from the local node's .spec.podCIDR)")
	_ = createCmd.MarkFlagRequired("name")
}
