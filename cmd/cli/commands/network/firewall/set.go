// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"strings"

	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Atomically replace a rule's full CIDR and/or port list",
	Long: "Replace the addresses and/or ports of one rule (--name), or of several reserved blocks at once via the " +
		"per-block flags. Every replacement in a single invocation lands as one nft transaction, so a `set` that " +
		"touches the management allowlist is never half-applied.\n\n" +
		"A flag left off leaves that list unchanged; a flag given an empty value clears it. Clearing a reserved " +
		"block's addresses is how you disable it without deleting it.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		updates, err := resolveSetUpdates(cmd)
		if err != nil {
			return err
		}
		return newManager().SetMany(cmd.Context(), updates)
	},
}

// resolveSetUpdates builds the replacement membership for this invocation. The
// per-block flags may name several reserved blocks in one call — that predates
// --name and stays supported — while --name addresses exactly one rule, which is
// the only form that can reach a named allow rule.
func resolveSetUpdates(cmd *cobra.Command) ([]fw.Update, error) {
	f := cmd.Flags()
	general := f.Changed("name") || f.Changed("cidrs") || f.Changed("cidrs-file") || f.Changed("ports")

	var legacy []fw.Update
	if f.Changed("mgmt-cidrs") {
		legacy = append(legacy, fw.Update{Name: fw.RuleMgmt, CIDRs: orEmpty(flagMgmtCIDRs)})
	}
	if f.Changed("blocked-cidrs") {
		legacy = append(legacy, fw.Update{Name: fw.RuleBlocked, CIDRs: orEmpty(flagBlockedCIDRs)})
	}
	if f.Changed("in-cluster-ports") {
		legacy = append(legacy, fw.Update{Name: fw.RuleInCluster, Ports: orEmpty(fw.PortStrings(flagInClusterPorts))})
	}

	switch {
	case len(legacy) > 0 && general:
		return nil, errorx.IllegalArgument.New(
			"--mgmt-cidrs, --blocked-cidrs and --in-cluster-ports already name the rule they replace; use --name with --cidrs/--ports instead of combining the two forms")
	case len(legacy) > 0:
		return legacy, nil
	case !f.Changed("name"):
		return nil, errorx.IllegalArgument.New(
			"--name is required: name a reserved block (%s) or an allow rule", strings.Join(fw.ReservedNames, ", "))
	}

	cidrs, err := resolveCIDRs(cmd)
	if err != nil {
		return nil, err
	}
	var ports []string
	if f.Changed("ports") {
		ports = orEmpty(flagPorts)
	}
	if cidrs == nil && ports == nil {
		return nil, errorx.IllegalArgument.New("at least one of --cidrs, --cidrs-file or --ports is required")
	}
	return []fw.Update{{Name: flagName, CIDRs: cidrs, Ports: ports}}, nil
}

// resolveCIDRs returns the replacement address list from --cidrs or --cidrs-file
// (mutually exclusive), or nil when neither was given, meaning "leave the
// addresses alone".
func resolveCIDRs(cmd *cobra.Command) ([]string, error) {
	f := cmd.Flags()
	switch {
	case f.Changed("cidrs") && f.Changed("cidrs-file"):
		return nil, errorx.IllegalArgument.New("--cidrs and --cidrs-file are mutually exclusive")
	case f.Changed("cidrs-file"):
		return readCIDRsFile(flagCIDRsFile)
	case f.Changed("cidrs"):
		return orEmpty(flagCIDRs), nil
	}
	return nil, nil
}

// readCIDRsFile reads a newline- and/or comma-separated CIDR list from a file,
// skipping blank lines and `#` comments. Deliberately the same flat format as
// `network policy --cidrs-file`, not the structured --from-file config: this is a
// bulk address list, and an operator pasting one should not have to wrap it in
// YAML. Per-entry syntax is validated downstream.
func readCIDRsFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to read --cidrs-file %s", path)
	}
	// Non-nil even when the file is empty: an empty --cidrs-file is an explicit
	// instruction to clear the list, not an instruction to leave it alone.
	out := []string{}
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

// orEmpty substitutes an empty slice for a nil one, so a flag that was set to an
// empty value clears the list rather than reading as "unchanged".
func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func init() {
	setCmd.Flags().StringVar(&flagName, "name", "",
		"Rule to replace: a reserved block (mgmt, blocked, in_cluster) or a named allow rule")
	setCmd.Flags().StringSliceVar(&flagCIDRs, "cidrs", nil, "Full CIDR list for --name (comma-separated; replaces the existing list)")
	setCmd.Flags().StringVar(&flagCIDRsFile, "cidrs-file", "", "Alternative to --cidrs: a file of CIDRs (one per line or comma-separated)")
	setCmd.Flags().StringSliceVar(&flagPorts, "ports", nil, "Full port list for --name; single ports and inclusive ranges (2379-2380) (comma-separated; replaces the existing list)")

	setCmd.Flags().StringSliceVar(&flagMgmtCIDRs, "mgmt-cidrs", nil, "Full management allowlist (comma-separated; replaces the existing list)")
	setCmd.Flags().StringSliceVar(&flagBlockedCIDRs, "blocked-cidrs", nil, "Full operator block list (comma-separated; replaces the existing list)")
	setCmd.Flags().IntSliceVar(&flagInClusterPorts, "in-cluster-ports", nil, "Full in-cluster host-service port list (comma-separated; replaces the existing list)")
}
