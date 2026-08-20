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
// can never diverge.
//
// membership carries the daemon-owned set contents, keyed by nft set name — so
// `bn-publisher`, its v6 companion `bn-publisher6`, and its listener-port set
// `bn-publisher_ports` are three separate keys. A set with no entry (or an empty
// one) renders as a bare schema, which is what an operator re-render produces
// before the daemon has ever polled. Rendering membership is what makes the sets
// boot-persistent: the shared nft oneshot replays this document and is ordered
// ahead of the daemon, so a quarantine peer is dropped from the first packet
// after a reboot rather than from the first successful statusz poll.
//
// Membership is supplied by the caller from live kernel state, never re-parsed
// out of the previous document. A caller that has no view of the kernel
// (RenderWeaverNft) renders bare schemas; the daemon's next successful poll
// replaces every daemon-owned set wholesale, so the artifact converges without
// a recovery path here.
//
// The hooked `forward` chain holds no rules of its own beyond a `meta nfproto`
// dispatch into forward_ipv4 / forward_ipv6, so a packet never evaluates rules
// belonging to the other address family. Its policy is `accept`: classification
// for the HTB hierarchy is what this table exists for, so traffic no rule
// matches falls through carrying no `meta priority` and lands in the HTB default
// class. The deny tier is the exception — those rules drop, and they are the
// only enforcement here (workload isolation at large is Cilium's).
//
// Rule position *within a family chain* is determined by action type and match
// specificity, never by creation order:
//
//  1. deny drops (both directions)
//  2. asymmetric reply-stamp restore
//  3. stamp classification — specific (has an IP-set match)
//  4. stamp classification — fallthrough (--from-entity world)
func Render(policies []*Policy, membership map[string][]string, podCIDRs ...string) (string, error) {
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

	setLines, err := renderSetDecls(policies, membership)
	if err != nil {
		return "", err
	}
	v4Lines, err := renderFamilyChain(policies, familyV4(podV4))
	if err != nil {
		return "", err
	}
	v6Lines, err := renderFamilyChain(policies, familyV6(podV6))
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
	b.WriteString(strings.Join(baseChainLines(), "\n"))
	b.WriteString("\n\n")
	writeChain(&b, chainV4, v4Lines)
	b.WriteString("\n")
	writeChain(&b, chainV6, v6Lines)
	b.WriteString("}\n")
	return b.String(), nil
}

// writeChain emits one regular chain. A chain with no rules renders as a bare
// `chain <name> { }` — reachable on a single-stack deployment with no deny
// policies, since the deny tier is the only one a family without a pod CIDR
// renders at all.
func writeChain(b *strings.Builder, name string, lines []string) {
	if len(lines) == 0 {
		b.WriteString("\tchain " + name + " { }\n")
		return
	}
	b.WriteString("\tchain " + name + " {\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\t}\n")
}

// Chain names. The hooked chain keeps its original name so `nft list chain inet
// weaver-workload-policy forward` and the operator docs still resolve.
const (
	chainBase = "forward"
	chainV4   = "forward_ipv4"
	chainV6   = "forward_ipv6"
)

// baseChainLines renders the hooked chain: a policy declaration and the family
// dispatch, nothing else.
//
// `meta nfproto` rather than `meta protocol`: in an `inet` table nfproto reads
// the netfilter protocol family straight off the hook, whereas `meta protocol`
// depends on the skb's ethertype being populated — not guaranteed for locally
// generated traffic or non-Ethernet interfaces.
func baseChainLines() []string {
	return []string{
		"\t# The hooked chain carries no rules of its own — it only dispatches into",
		"\t# the per-family chains below, so an IPv4 packet never evaluates an IPv6",
		"\t# rule and vice versa.",
		"\t#",
		"\t# Policy is `accept` because this table classifies traffic for the HTB",
		"\t# hierarchy: a packet no rule matches falls through carrying no",
		"\t# `meta priority` and lands in the HTB default class. The deny tier in the",
		"\t# chains below is the exception — those rules drop.",
		"\tchain " + chainBase + " {",
		"\t\ttype filter hook forward priority 0; policy accept;",
		"\t\tmeta nfproto vmap { ipv4 : jump " + chainV4 + ", ipv6 : jump " + chainV6 + " }",
		"\t}",
	}
}

// family carries the per-address-family spelling differences between the two
// generated chains: the nft L3 match keyword, that family's pod CIDR (empty on a
// single-stack deployment's absent family, which suppresses its stamp rules),
// and the selector for a policy's membership set.
type family struct {
	proto   string
	podCIDR string
	setName func(string) string
}

func familyV4(podCIDR string) family {
	return family{proto: "ip", podCIDR: podCIDR, setName: func(name string) string { return name }}
}

func familyV6(podCIDR string) family {
	return family{proto: "ip6", podCIDR: podCIDR, setName: V6SetName}
}

// needsPodCIDR reports whether any policy in the set renders a pod-scoped rule:
// every --stamp policy, plus a --deny policy that carries --ports (see
// renderDenyRules). A registry of membership-only deny policies never references
// POD_CIDR, so it shouldn't be required to render one.
func needsPodCIDR(policies []*Policy) bool {
	for _, p := range policies {
		switch p.Action {
		case ActionStamp:
			return true
		case ActionDeny:
			if p.hasPortsSet() {
				return true
			}
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
// deterministic render, seeding each daemon-owned set with the membership
// supplied for it. A static `--ports` set keeps rendering its operator-declared
// elements from the registry, not from membership — those are part of the policy
// definition, not runtime state.
func renderSetDecls(policies []*Policy, membership map[string][]string) ([]string, error) {
	var lines []string
	for _, p := range policies {
		if p.hasCIDRSet() {
			v4, v6 := p.Name, V6SetName(p.Name)
			if p.isCompoundSet() {
				// Compound ip:port key for --reply-stamp destinations, one set
				// per family (the v6 set carries the "6"-suffixed name).
				lines = append(lines,
					setDecl(v4, "ipv4_addr . inet_service", membership[v4]),
					setDecl(v6, "ipv6_addr . inet_service", membership[v6]))
			} else {
				// `flags interval` so the set can hold CIDRs, but deliberately
				// NO `auto-merge` -- unlike the host-firewall template
				// (network-weaver-host-firewall.nft.tmpl), which has both.
				//
				// auto-merge is safe there because that table re-renders from an
				// authoritative persisted YAML config on every mutation, so a
				// folded kernel read-back costs nothing (#1002/#1004). It is not
				// safe here, and persisting membership into this document does
				// NOT make it so: what is written back is a snapshot of the live
				// sets, so anything the kernel folded is folded in the artifact
				// too. The kernel stays the authority for membership; the
				// document is a replay of it.
				//
				// Two things would break if it were switched on (neither is a
				// live problem — this is why the flag stays off). `delete
				// element` would lose the exact element to remove, so an operator
				// could no longer withdraw a /32 that got absorbed into a /24.
				// And the daemon diffs desired-from-statusz against live every
				// tick: statusz reports individual peers, which are routinely
				// consecutive, so folding 10.0.0.1 and 10.0.0.2 into one range
				// would yield a live set that can never equal the desired list --
				// a delta on every tick.
				//
				// Note the containment handling below does NOT make auto-merge
				// viable: it prunes/rejects a prefix COVERED by another, while
				// auto-merge additionally folds merely ADJACENT prefixes, which
				// nothing here produces or expects. Enabling it would first need
				// the desired side folded by the same algorithm before diffing.
				//
				// Overlapping membership is handled in Go instead (cidrset.go):
				// operator-authored lists are rejected naming both prefixes,
				// daemon-derived lists have covered entries pruned.
				lines = append(lines,
					setDecl(v4, "ipv4_addr; flags interval", membership[v4]),
					setDecl(v6, "ipv6_addr; flags interval", membership[v6]))
			}
		}
		if len(p.Ports) > 0 {
			lines = append(lines, fmt.Sprintf("\tset %s { type inet_service; elements = { %s }; }",
				PortsSetName(p.Name), portElements(p.Ports)))
		} else if p.ManagedPorts {
			// Daemon-managed listener ports, filled from the BN's statusz
			// local.port. Seeded here from the last applied state for the same
			// reason the CIDR sets are: until the first poll lands, an empty set
			// makes the `tcp dport @<name>_ports` clause match nothing.
			lines = append(lines, setDecl(PortsSetName(p.Name), "inet_service", membership[PortsSetName(p.Name)]))
		}
	}
	return lines, nil
}

// setDecl renders one set declaration, appending an `elements = { … }` clause
// when the set has members. Elements are canonicalized so the render is a pure
// function of set contents — RenderWeaverNft's SHA-256 skip and the daemon's
// change detection both depend on an unchanged membership producing an
// unchanged document.
func setDecl(name, typeSpec string, elements []string) string {
	canon := CanonicalizeElements(elements)
	if len(canon) == 0 {
		return fmt.Sprintf("\tset %s { type %s; }", name, typeSpec)
	}
	return fmt.Sprintf("\tset %s { type %s; elements = { %s }; }", name, typeSpec, strings.Join(canon, ", "))
}

// renderFamilyChain builds one address family's chain body (indented two tabs),
// grouped into the four tiers above. Both chains are rendered from the same
// policy set; f decides how each rule is spelled and which membership set it
// references, so no rule can match a family it cannot belong to.
func renderFamilyChain(policies []*Policy, f family) ([]string, error) {
	var lines []string

	// Tier 1: drops, both directions.
	var deny []string
	for _, p := range policies {
		if p.Action == ActionDeny {
			deny = append(deny, renderDenyRules(p, f)...)
		}
	}
	if len(deny) > 0 {
		lines = appendSection(lines,
			"\t\t# Deny. Runs ahead of the classification accepts below so a denied",
			"\t\t# packet is dropped rather than stamped. There is no conntrack accept",
			"\t\t# fast-path in this chain, so packets on already-open connections are",
			"\t\t# evaluated against these drops too. A membership deny drops both",
			"\t\t# directions; a port-scoped deny drops the request leg only.")
		lines = append(lines, deny...)
	}

	// Tier 2: asymmetric reply-stamp restore. Gated on this family's pod CIDR for
	// the same reason as the stamp tiers below: the ct mark it matches is only ever
	// written by renderStampRule, which is itself pod-CIDR-gated, so in a family
	// with no pod CIDR no packet can carry the mark and the rule is unreachable.
	var restore []string
	if f.podCIDR != "" {
		for _, p := range policies {
			if p.Action == ActionStamp && p.ReplyStamp != "" {
				rule, err := renderReplyRestoreRule(p)
				if err != nil {
					return nil, err
				}
				restore = append(restore, rule)
			}
		}
	}
	if len(restore) > 0 {
		lines = appendSection(lines,
			"\t\t# Asymmetric reply restore. Matches conntrack only, so it is spelled",
			"\t\t# identically in both family chains that have a pod CIDR. Must precede",
			"\t\t# the classification tiers: on the reply the addresses are reversed, so a",
			"\t\t# broad fallthrough rule below would otherwise claim the packet and stamp",
			"\t\t# it with the forward class instead of the reply class.")
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
		rule, err := renderStampRule(p, f)
		if err != nil {
			return nil, err
		}
		if rule != "" {
			specific = append(specific, rule)
		}
	}
	for _, p := range fallthrPolicies {
		rule, err := renderStampRule(p, f)
		if err != nil {
			return nil, err
		}
		if rule != "" {
			fallthr = append(fallthr, rule)
		}
	}
	if len(specific) > 0 {
		lines = appendSection(lines, "\t\t# Classification — specific matches.")
		lines = append(lines, specific...)
	}
	if len(fallthr) > 0 {
		lines = appendSection(lines, "\t\t# Classification — fallthrough (any source/dest).")
		lines = append(lines, fallthr...)
	}

	return lines, nil
}

// appendSection appends a tier's comment block, separated from any preceding
// tier by a blank line. A chain whose first tier is empty (a stamp-only registry
// has no deny rules) therefore does not open with a stray blank line.
func appendSection(lines []string, comment ...string) []string {
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	return append(lines, comment...)
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

// renderDenyRules renders one address family's drop rules for a deny policy.
//
// Without --ports it is the plain membership drop, both directions: the set is
// the whole match, so there is nothing to scope to the pod CIDR.
//
// A deny that carries --ports drops the *request* leg of a connection to a
// workload listener port, scoped to the pod CIDR like the stamp tiers — and
// returns nil for a family with no pod CIDR, for the same reason they do. It
// keeps its membership clause when it has one; with --from-entity world it drops
// that port from every source, which is what makes a workload port unreachable
// from off-node without enumerating the peers that may reach it.
//
// `ct direction original` is load-bearing. The port set holds listener ports,
// and those sit inside the default ephemeral range
// (net.ipv4.ip_local_port_range, 32768-60999). Without it, the reply leg of an
// unrelated connection that happened to draw a listener port as its ephemeral
// source port matches `daddr <podCIDR> tcp dport <listener>` and is dropped —
// silently killing that connection, since SYN retransmits reuse the port. That
// packet's conntrack direction is `reply`, so the qualifier excludes it while
// still matching a genuine inbound connection to the listener. Conntrack is
// always available here: the hook at priority -200 runs before this chain, and
// the reply-restore tier below already reads `ct direction`.
//
// There is deliberately no egress mirror. Dropping the request leg is enough —
// no request means no reply, and for a connection opened before the rule existed
// this chain has no conntrack accept fast-path, so its forward leg is dropped
// too. An egress rule matching `saddr <podCIDR> tcp sport <listener>` would
// reintroduce the same ephemeral collision on the outbound leg, where no
// direction qualifier can exclude it.
//
// Clause order is an evaluation-cost choice, not a semantic one: the pod CIDR
// prefix compare rejects most forwarded packets before the port set lookup and
// the conntrack read.
func renderDenyRules(p *Policy, f family) []string {
	if !p.hasPortsSet() {
		return []string{
			fmt.Sprintf("\t\t%s saddr @%s drop", f.proto, f.setName(p.Name)),
			fmt.Sprintf("\t\t%s daddr @%s drop", f.proto, f.setName(p.Name)),
		}
	}
	if f.podCIDR == "" {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\t\t%s daddr %s", f.proto, f.podCIDR)
	if p.hasCIDRSet() {
		fmt.Fprintf(&b, " %s saddr @%s", f.proto, f.setName(p.Name))
	}
	fmt.Fprintf(&b, " tcp dport @%s ct direction original drop", PortsSetName(p.Name))
	return []string{b.String()}
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

// renderStampRule renders one address family's classification rule for a stamp
// policy, honoring --from-entity world (no IP-set clause) and --reply-stamp
// (compound-key egress forward rule with a ct mark write). It returns "" when
// this family has no pod CIDR, so a single-stack deployment renders only its own
// family's rules while the other family's chain carries the deny tier alone.
func renderStampRule(p *Policy, f family) (string, error) {
	if f.podCIDR == "" {
		return "", nil
	}
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
		return fmt.Sprintf("\t\t%s saddr %s %s daddr . tcp dport @%s ct mark set %s meta priority set %s accept",
			f.proto, f.podCIDR, f.proto, f.setName(p.Name), hex(reply.Mark), hex(fwd.Priority)), nil
	}

	return renderPlainStampRule(p, f, fwd)
}

// renderPlainStampRule renders one address family's plain stamp classification
// rule. The listener-ports set is a shared family-agnostic inet_service set, so
// it is referenced by the same @<name>_ports name in both families.
func renderPlainStampRule(p *Policy, f family, fwd class) (string, error) {
	var b strings.Builder
	b.WriteString("\t\t")
	switch p.Direction {
	case DirectionIngress:
		b.WriteString(f.proto + " daddr " + f.podCIDR)
		if p.hasCIDRSet() {
			b.WriteString(" " + f.proto + " saddr @" + f.setName(p.Name))
		}
		if p.hasPortsSet() {
			b.WriteString(" tcp dport @" + PortsSetName(p.Name))
		}
	case DirectionEgress:
		b.WriteString(f.proto + " saddr " + f.podCIDR)
		if p.hasCIDRSet() {
			b.WriteString(" " + f.proto + " daddr @" + f.setName(p.Name))
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

	tmp, err := os.CreateTemp(dir, ".network-weaver-workload-policy-*.tmp")
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
