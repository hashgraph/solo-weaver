// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joomcode/errorx"
)

// Render produces the full `inet weaver` nft document for the given set of
// registry policies, in tier order. The same output feeds both the kernel apply
// (`nft -f`) and the on-disk artifact, so the live table and the persisted file
// can never diverge. Set *membership* is deliberately not rendered here — only
// set schemas and the static `--ports` elements — because membership is owned
// by the daemon poll loop and never persisted.
//
// Rule position is determined by action type and match specificity, never by
// creation order:
//
//  1. deny drops (both directions)
//  2. asymmetric reply-stamp restore
//  3. stamp classification — specific (has an IP-set match)
//  4. stamp classification — fallthrough (--from-entity world)
//  5. unclassified pod egress                (structural; only when podCIDR is set)
//  6. ct state established,related accept   (structural)
//  7. drop                                   (structural)
func Render(policies []*Policy, podCIDR string) (string, error) {
	if podCIDR == "" && needsPodCIDR(policies) {
		return "", errorx.IllegalArgument.New("pod CIDR is required to render a --stamp policy in the inet weaver chain")
	}

	// renderSetDecls's own doc comment promises name-sorted output; sort
	// here (a copy, so the caller's slice is untouched) rather than relying
	// on every caller to pre-sort -- Manager.Create's upsert already does,
	// but Render is exported and callers other than Create (tests, a future
	// show/reconcile path) shouldn't have to replicate that to get a
	// deterministic render.
	policies = sortedByName(policies)

	setLines, err := renderSetDecls(policies)
	if err != nil {
		return "", err
	}
	chainLines, err := renderChain(policies, podCIDR)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	// The idempotent prefix makes a re-apply atomically replace the table (same
	// convention as internal/network/firewall) so this document is safe for both
	// the boot oneshot and live re-applies.
	b.WriteString("add table " + TableName + "\n")
	b.WriteString("delete table " + TableName + "\n")
	b.WriteString("add table " + TableName + "\n")
	b.WriteString("table " + TableName + " {\n")
	if len(setLines) > 0 {
		b.WriteString(strings.Join(setLines, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString("\tchain forward {\n")
	b.WriteString(strings.Join(chainLines, "\n"))
	b.WriteString("\n\t}\n")
	b.WriteString("}\n")
	return b.String(), nil
}

// needsPodCIDR reports whether any policy in the set is a --stamp policy.
// POD_CIDR is only ever read by renderStampRule; a deny-only chain never
// references it, so it shouldn't be required to render one.
func needsPodCIDR(policies []*Policy) bool {
	for _, p := range policies {
		if p.Action == ActionStamp {
			return true
		}
	}
	return false
}

// sortedByName returns a name-sorted copy of policies, leaving the input
// slice untouched.
func sortedByName(policies []*Policy) []*Policy {
	out := make([]*Policy, len(policies))
	copy(out, policies)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// renderSetDecls emits the schema for each policy's sets, name-sorted for a
// deterministic render. Membership set elements are omitted; only the static
// `--ports` set carries elements.
func renderSetDecls(policies []*Policy) ([]string, error) {
	var lines []string
	for _, p := range policies {
		if p.hasCIDRSet() {
			if p.isCompoundSet() {
				// Compound ip:port key for --reply-stamp destinations.
				lines = append(lines, fmt.Sprintf("\tset %s { type ipv4_addr . inet_service; }", p.Name))
			} else {
				lines = append(lines, fmt.Sprintf("\tset %s { type ipv4_addr; flags interval; }", p.Name))
			}
		}
		if len(p.Ports) > 0 {
			lines = append(lines, fmt.Sprintf("\tset %s_ports { type inet_service; elements = { %s }; }",
				p.Name, portElements(p.Ports)))
		}
	}
	return lines, nil
}

// renderChain builds the forward chain body lines (indented two tabs), grouped
// into the seven tiers above.
func renderChain(policies []*Policy, podCIDR string) ([]string, error) {
	lines := []string{
		"\t\ttype filter hook forward priority 0; policy drop;",
		"\t\tct state invalid drop",
	}

	// Tier 1: quarantine drops.
	var deny []string
	for _, p := range policies {
		if p.Action == ActionDeny {
			deny = append(deny,
				fmt.Sprintf("\t\tip saddr @%s drop", p.Name),
				fmt.Sprintf("\t\tip daddr @%s drop", p.Name))
		}
	}
	if len(deny) > 0 {
		lines = append(lines, "",
			"\t\t# Quarantine (deny), both directions. Runs before est,rel accept",
			"\t\t# so already-open connections are also killed.")
		lines = append(lines, deny...)
	}

	// Tier 2: asymmetric reply-stamp restore.
	var restore []string
	for _, p := range policies {
		if p.Action == ActionStamp && p.ReplyStamp != "" {
			rule, err := renderReplyRestoreRule(p)
			if err != nil {
				return nil, err
			}
			restore = append(restore, rule)
		}
	}
	if len(restore) > 0 {
		lines = append(lines, "",
			"\t\t# Asymmetric reply restore. Must precede est,rel accept so every",
			"\t\t# reply packet is reclassified, not just the SYN.")
		lines = append(lines, restore...)
	}

	// Tiers 3 and 4: stamp classification, specific before fallthrough. Within
	// each tier, policies are grouped by (Direction, Ports) and ordered by
	// CreatedAt within a group -- a stable tiebreaker for the cases that can
	// still share a group (fallthrough policies, which the overlap check in
	// Manager.Create doesn't apply to; or specific policies loaded from
	// registry data written before that check existed).
	var specificPolicies, fallthrPolicies []*Policy
	for _, p := range policies {
		if p.Action != ActionStamp {
			continue
		}
		if p.FromEntityWorld {
			fallthrPolicies = append(fallthrPolicies, p)
		} else {
			specificPolicies = append(specificPolicies, p)
		}
	}
	specificPolicies = orderByGroupThenCreatedAt(specificPolicies)
	fallthrPolicies = orderByGroupThenCreatedAt(fallthrPolicies)

	var specific, fallthr []string
	for _, p := range specificPolicies {
		rule, err := renderStampRule(p, podCIDR)
		if err != nil {
			return nil, err
		}
		specific = append(specific, rule)
	}
	for _, p := range fallthrPolicies {
		rule, err := renderStampRule(p, podCIDR)
		if err != nil {
			return nil, err
		}
		fallthr = append(fallthr, rule)
	}
	if len(specific) > 0 {
		lines = append(lines, "", "\t\t# Classification — specific matches.")
		lines = append(lines, specific...)
	}
	if len(fallthr) > 0 {
		lines = append(lines, "", "\t\t# Classification — fallthrough (any source/dest).")
		lines = append(lines, fallthr...)
	}

	// Tier 5: unclassified pod egress. Only rendered when a pod CIDR is known
	// (the canonical BN install set always supplies one; a deny-only chain
	// built without one just skips this tier rather than erroring, since
	// nothing above requires it either). Deliberately no meta priority: this
	// is a default-allow escape hatch for outbound traffic this registry
	// doesn't otherwise classify (e.g. the chart's Maven-based plugin
	// resolution), so it falls to the HTB default class instead of one of
	// the classified priority bands. ip daddr != podCIDR keeps this scoped to
	// egress leaving the node, not a blanket pass for intra-CIDR pod-to-pod
	// traffic the classification tiers above don't already cover.
	if podCIDR != "" {
		lines = append(lines, "",
			"\t\t# Unclassified pod egress (no meta priority; HTB default class).",
			fmt.Sprintf("\t\tip saddr %s ip daddr != %s accept", podCIDR, podCIDR))
	}

	lines = append(lines, "",
		"\t\tct state established,related accept",
		"\t\tdrop")
	return lines, nil
}

// orderByGroupThenCreatedAt returns policies reordered so that members of the
// same (Direction, Ports) group (see groupKey) are contiguous and sorted by
// CreatedAt ascending, while groups themselves keep the relative order of
// their first member in the input. The per-group sort is stable, so policies
// with equal CreatedAt (e.g. an unpopulated field in older test fixtures)
// retain their input order rather than being shuffled.
func orderByGroupThenCreatedAt(policies []*Policy) []*Policy {
	type group struct {
		key     string
		members []*Policy
	}
	var groups []*group
	byKey := make(map[string]*group, len(policies))
	for _, p := range policies {
		k := groupKey(p)
		g, ok := byKey[k]
		if !ok {
			g = &group{key: k}
			byKey[k] = g
			groups = append(groups, g)
		}
		g.members = append(g.members, p)
	}

	out := make([]*Policy, 0, len(policies))
	for _, g := range groups {
		sort.SliceStable(g.members, func(i, j int) bool {
			return g.members[i].CreatedAt.Before(g.members[j].CreatedAt)
		})
		out = append(out, g.members...)
	}
	return out
}

// renderReplyRestoreRule renders the ingress restore rule for a --reply-stamp
// policy: on the conntrack reply, restamp with the reply class's priority.
func renderReplyRestoreRule(p *Policy) (string, error) {
	reply, err := lookupClass(p.ReplyStamp)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("\t\tct direction reply ct mark %s meta priority set %s accept",
		hex(reply.Mark), hex(reply.Priority)), nil
}

// renderStampRule renders a single stamp policy's classification rule for its
// direction, honoring --from-entity world (no IP-set clause) and --reply-stamp
// (compound-key egress forward rule with a ct mark write).
func renderStampRule(p *Policy, podCIDR string) (string, error) {
	fwd, err := lookupClass(p.Stamp)
	if err != nil {
		return "", err
	}

	if p.isCompoundSet() {
		// --reply-stamp forward rule: egress, compound ip:port destination key,
		// ct mark write for the reply restore to read back.
		reply, err := lookupClass(p.ReplyStamp)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("\t\tip saddr %s ip daddr . tcp dport @%s ct mark set %s meta priority set %s accept",
			podCIDR, p.Name, hex(reply.Mark), hex(fwd.Priority)), nil
	}

	var b strings.Builder
	b.WriteString("\t\t")
	switch p.Direction {
	case DirectionIngress:
		b.WriteString("ip daddr " + podCIDR)
		if p.hasCIDRSet() {
			b.WriteString(" ip saddr @" + p.Name)
		}
		if len(p.Ports) > 0 {
			b.WriteString(" tcp dport @" + p.Name + "_ports")
		}
	case DirectionEgress:
		b.WriteString("ip saddr " + podCIDR)
		if p.hasCIDRSet() {
			b.WriteString(" ip daddr @" + p.Name)
		}
		if len(p.Ports) > 0 {
			b.WriteString(" tcp sport @" + p.Name + "_ports")
		}
	default:
		return "", errorx.AssertionFailed.New("stamp policy %q has no direction", p.Name)
	}
	b.WriteString(fmt.Sprintf(" meta priority set %s accept", hex(fwd.Priority)))
	return b.String(), nil
}

// hex formats an nft numeric literal as lowercase hex (e.g. 0x10010). This is
// what we write and what the golden file pins. `nft list table` reformats a
// `meta priority` value that decodes as a valid tc classid into its
// `major:minor` display form on read-back (e.g. 0x10010 -> "1:10") -- that's
// nft's own listing behavior, not a discrepancy in the rendered document.
func hex(v uint32) string { return fmt.Sprintf("0x%x", v) }

// atomicWriteFile writes content to path via a temp file in the same directory
// followed by fsync + rename + parent-dir fsync, so a crash mid-write can never
// leave a torn file that the boot oneshot would fail to load. Mirrors the
// firewall package's writer (a shared helper is a follow-up refactor).
func atomicWriteFile(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}

	tmp, err := os.CreateTemp(dir, ".network-weaver-*.tmp")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create temp file in %s", dir)
	}
	tmpName := tmp.Name()

	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return errorx.ExternalError.Wrap(err, "failed to write temp file %s", tmpName)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errorx.ExternalError.Wrap(err, "failed to fsync temp file %s", tmpName)
	}
	if err := tmp.Close(); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to close temp file %s", tmpName)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to chmod temp file %s", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to rename %s to %s", tmpName, path)
	}
	committed = true

	d, err := os.Open(dir)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to open directory %s for fsync", dir)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to fsync directory %s", dir)
	}
	return nil
}
