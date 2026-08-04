// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// Render produces the full `inet weaver-workload-policy` nft document for the given set of
// registry policies, in tier order. The same output feeds both the kernel apply
// (`nft -f`) and the on-disk artifact, so the live table and the persisted file
// can never diverge. Set *membership* is deliberately not rendered here — only
// set schemas and any static `--ports` elements — because membership is owned
// by the daemon poll loop and never persisted. A managed-ports set
// (`<name>_ports` for a ManagedPorts policy) is likewise declared empty here and
// filled from statusz at runtime, exactly like the CIDR membership set.
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
func Render(policies []*Policy, podCIDRs ...string) (string, error) {
	podV4, podV6 := partitionPodCIDRs(podCIDRs)
	if podV4 == "" && podV6 == "" && needsPodCIDR(policies) {
		return "", errorx.IllegalArgument.New("pod CIDR is required to render a --stamp policy in the inet weaver-workload-policy chain")
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
	chainLines, err := renderChain(policies, podV4, podV6)
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

// nonEmptyStrings returns in with empty entries removed, allocating a fresh
// slice so the caller's argument is untouched.
func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// partitionPodCIDRs splits the supplied pod CIDRs into one IPv4 and one IPv6
// value by family (the last of each family wins), ignoring empties. A dual-stack
// deployment supplies both, so the chain renders both `ip` and `ip6` stamp/egress
// rules; a single-stack one supplies one and the other family's rules are simply
// not rendered (its v6/v4 set stays declared but unreferenced). A value that
// fails classification is treated as v4 so a stamp chain still renders rather
// than silently dropping the pod scope.
func partitionPodCIDRs(podCIDRs []string) (v4, v6 string) {
	for _, c := range podCIDRs {
		if c == "" {
			continue
		}
		if isV6, err := sanity.CIDRIsIPv6(c); err == nil && isV6 {
			v6 = c
		} else {
			v4 = c
		}
	}
	return v4, v6
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
// deterministic render. Membership set elements are omitted; only a static
// `--ports` set carries inline elements — a managed-ports set is declared empty
// and filled by the daemon.
func renderSetDecls(policies []*Policy) ([]string, error) {
	var lines []string
	for _, p := range policies {
		if p.hasCIDRSet() {
			if p.isCompoundSet() {
				// Compound ip:port key for --reply-stamp destinations, one set
				// per family (the v6 set carries the "6"-suffixed name).
				lines = append(lines,
					fmt.Sprintf("\tset %s { type ipv4_addr . inet_service; }", p.Name),
					fmt.Sprintf("\tset %s { type ipv6_addr . inet_service; }", V6SetName(p.Name)))
			} else {
				lines = append(lines,
					fmt.Sprintf("\tset %s { type ipv4_addr; flags interval; }", p.Name),
					fmt.Sprintf("\tset %s { type ipv6_addr; flags interval; }", V6SetName(p.Name)))
			}
		}
		if len(p.Ports) > 0 {
			lines = append(lines, fmt.Sprintf("\tset %s { type inet_service; elements = { %s }; }",
				PortsSetName(p.Name), portElements(p.Ports)))
		} else if p.ManagedPorts {
			// Daemon-managed listener ports: the set is declared but empty,
			// exactly like the CIDR membership set above. The traffic-shaper
			// poll loop fills it from the BN's statusz local.port each tick;
			// nothing is seeded (or persisted) here. Until the first poll the
			// set is empty, so the `tcp dport @<name>_ports` clause matches
			// nothing — the same bootstrap behavior the membership sets have.
			lines = append(lines, fmt.Sprintf("\tset %s { type inet_service; }", PortsSetName(p.Name)))
		}
	}
	return lines, nil
}

// renderChain builds the forward chain body lines (indented two tabs), grouped
// into the seven tiers above.
func renderChain(policies []*Policy, podV4, podV6 string) ([]string, error) {
	lines := []string{
		"\t\ttype filter hook forward priority 0; policy drop;",
		"\t\tct state invalid drop",
	}

	// Tier 1: quarantine drops, both families.
	var deny []string
	for _, p := range policies {
		if p.Action == ActionDeny {
			deny = append(deny,
				fmt.Sprintf("\t\tip saddr @%s drop", p.Name),
				fmt.Sprintf("\t\tip daddr @%s drop", p.Name),
				fmt.Sprintf("\t\tip6 saddr @%s drop", V6SetName(p.Name)),
				fmt.Sprintf("\t\tip6 daddr @%s drop", V6SetName(p.Name)))
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
		rules, err := renderStampRule(p, podV4, podV6)
		if err != nil {
			return nil, err
		}
		specific = append(specific, rules...)
	}
	for _, p := range fallthrPolicies {
		rules, err := renderStampRule(p, podV4, podV6)
		if err != nil {
			return nil, err
		}
		fallthr = append(fallthr, rules...)
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
	// the classified priority bands.
	if podV4 != "" || podV6 != "" {
		lines = append(lines, "",
			"\t\t# Unclassified pod egress (no meta priority; HTB default class).")
		if podV4 != "" {
			lines = append(lines, fmt.Sprintf("\t\tip saddr %s accept", podV4))
		}
		if podV6 != "" {
			lines = append(lines, fmt.Sprintf("\t\tip6 saddr %s accept", podV6))
		}
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

// renderStampRule renders a stamp policy's classification rule(s) for its
// direction, honoring --from-entity world (no IP-set clause) and --reply-stamp
// (compound-key egress forward rule with a ct mark write). It emits one rule per
// pod-CIDR family supplied: an `ip` rule (against @<name>) when podV4 is set and
// an `ip6` rule (against @<name>6) when podV6 is set, so a dual-stack deployment
// classifies both families and a single-stack one renders only its family.
func renderStampRule(p *Policy, podV4, podV6 string) ([]string, error) {
	fwd, err := lookupClass(p.Stamp)
	if err != nil {
		return nil, err
	}

	var rules []string

	if p.isCompoundSet() {
		// --reply-stamp forward rule: egress, compound ip:port destination key,
		// ct mark write for the reply restore to read back. One per pod family.
		reply, err := lookupClass(p.ReplyStamp)
		if err != nil {
			return nil, err
		}
		if podV4 != "" {
			rules = append(rules, fmt.Sprintf("\t\tip saddr %s ip daddr . tcp dport @%s ct mark set %s meta priority set %s accept",
				podV4, p.Name, hex(reply.Mark), hex(fwd.Priority)))
		}
		if podV6 != "" {
			rules = append(rules, fmt.Sprintf("\t\tip6 saddr %s ip6 daddr . tcp dport @%s ct mark set %s meta priority set %s accept",
				podV6, V6SetName(p.Name), hex(reply.Mark), hex(fwd.Priority)))
		}
		return rules, nil
	}

	if podV4 != "" {
		rule, err := renderPlainStampRule(p, podV4, "ip", p.Name, fwd)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if podV6 != "" {
		rule, err := renderPlainStampRule(p, podV6, "ip6", V6SetName(p.Name), fwd)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// renderPlainStampRule renders one address family's plain stamp classification
// rule. proto is the nft L3 keyword ("ip" or "ip6"); setName is that family's
// membership set (@<name> or @<name>6). The listener-ports set is a shared
// family-agnostic inet_service set, so it is referenced by the same @<name>_ports
// name in both families.
func renderPlainStampRule(p *Policy, podCIDR, proto, setName string, fwd class) (string, error) {
	var b strings.Builder
	b.WriteString("\t\t")
	switch p.Direction {
	case DirectionIngress:
		b.WriteString(proto + " daddr " + podCIDR)
		if p.hasCIDRSet() {
			b.WriteString(" " + proto + " saddr @" + setName)
		}
		if p.hasPortsSet() {
			b.WriteString(" tcp dport @" + PortsSetName(p.Name))
		}
	case DirectionEgress:
		b.WriteString(proto + " saddr " + podCIDR)
		if p.hasCIDRSet() {
			b.WriteString(" " + proto + " daddr @" + setName)
		}
		if p.hasPortsSet() {
			b.WriteString(" tcp sport @" + PortsSetName(p.Name))
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
