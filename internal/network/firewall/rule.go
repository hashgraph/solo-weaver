// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"sort"
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// Proto is the L4 protocol a rule matches. A rule names exactly one: nft has no
// combined tcp/udp dport match, so a service reachable over both families of
// protocol is two rules.
type Proto string

const (
	// ProtoTCP matches TCP destination ports.
	ProtoTCP Proto = "tcp"
	// ProtoUDP matches UDP destination ports.
	ProtoUDP Proto = "udp"
)

// Reserved rule names. These three are first-class rather than operator-authored
// because weaver derives or defaults their content and omitting them is
// dangerous: an empty mgmt list locks the operator out, an absent in-cluster
// list breaks the cluster, and the block list renders on three hooks rather than
// one. Everything else is an ordinary named allow rule.
const (
	// RuleMgmt is the management allowlist: the only source of host-local
	// administrative access under the input chain's default drop.
	RuleMgmt = "mgmt"
	// RuleBlocked is the operator-curated deny list. It renders on prerouting,
	// input and output, so it is not expressible as an allow rule.
	RuleBlocked = "blocked"
	// RuleInCluster is the pod-CIDR-to-host-service allowance. Its address list
	// is auto-detected from the node's .spec.podCIDR when the operator omits it.
	RuleInCluster = "in_cluster"
)

// ReservedNames are the rule names an `allow` entry may not take, in render
// order.
var ReservedNames = []string{RuleMgmt, RuleBlocked, RuleInCluster}

// Rule is one named record in the host firewall: a source address list, a
// destination port list, and the protocol they apply to. The three reserved
// names render into fixed positions and ignore some fields (see Validate); an
// allow rule renders uniformly as
// `<family> saddr @<name> <proto> dport @<name>_ports accept`.
//
// Ports are strings, not ints, so an inclusive range ("2379-2380") is
// expressible without a mixed int/string list. CIDRs may mix address families;
// the renderer routes each to the matching per-family set.
type Rule struct {
	Name  string   `yaml:"name" json:"name"`
	CIDRs []string `yaml:"cidrs,omitempty" json:"cidrs,omitempty"`
	Ports []string `yaml:"ports,omitempty" json:"ports,omitempty"`
	// Proto defaults to ProtoTCP when empty. Meaningless on mgmt (which renders
	// a fixed TCP accept plus its own ICMP type list) and on blocked (which
	// drops every protocol).
	Proto Proto `yaml:"proto,omitempty" json:"proto,omitempty"`
	// ICMPEcho grants this rule's sources unmetered echo-request, rendered into
	// the per-family ICMP chains above the rate meter. Meaningless on mgmt,
	// which already carries a broader ICMP type list, and on blocked.
	ICMPEcho bool `yaml:"icmp_echo,omitempty" json:"icmp_echo,omitempty"`
}

// IsReserved reports whether name is one of the three reserved blocks.
func IsReserved(name string) bool {
	return sanity.Contains(name, ReservedNames)
}

// addrSetName returns the nft address-set name holding a rule's IPv4 members.
// The reserved blocks keep the `_addrs` suffix they shipped with — those names
// appear in the ICMP chains, in the docs, and in the on-disk artifact of every
// already-provisioned host — while an allow rule uses its bare name, matching
// the workload plane's `@bn-publisher` convention.
func addrSetName(name string) string {
	if IsReserved(name) {
		return name + "_addrs"
	}
	return name
}

// v6SetName returns the IPv6 companion of addrSetName. Every derived set name
// goes through one of these three functions so the renderer, the parser and the
// collision check can never disagree on a spelling.
func v6SetName(name string) string { return addrSetName(name) + "6" }

// portsSetName returns the nft set name holding a rule's destination ports.
func portsSetName(name string) string { return name + "_ports" }

// incomplete reports whether an allow rule is declared but not yet populated
// enough to emit anything. It mirrors the template's own gating: a rule needs at
// least one source address, and either a destination port or an echo accept,
// before any line is rendered for it. Only meaningful for allow rules — the
// reserved blocks render fixed positions and an empty one is a deliberate
// "disabled", not an unfinished declaration.
func (r *Rule) incomplete() bool {
	if IsReserved(r.Name) {
		return false
	}
	return len(r.CIDRs) == 0 || (len(r.Ports) == 0 && !r.ICMPEcho)
}

// proto returns the rule's effective protocol, applying the tcp default.
func (r *Rule) proto() Proto {
	if r.Proto == "" {
		return ProtoTCP
	}
	return r.Proto
}

// Validate rejects any field that would be unsafe or nonsensical to render.
// Every untrusted token goes through pkg/sanity, so a malformed value can never
// break the atomic nft transaction or smuggle in nft syntax. flagFor names the
// CLI flag in the error so an operator sees the input they supplied rather than
// an internal field name.
func (r *Rule) Validate() error {
	if err := sanity.ValidateIdentifier(r.Name); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid rule name %q", r.Name)
	}

	cidrFlag, portFlag := r.flagNames()
	for _, c := range r.CIDRs {
		if err := sanity.ValidateCIDR(c); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid %s %q", cidrFlag, c)
		}
	}
	for _, p := range r.Ports {
		if err := validatePortSpec(p); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid %s %q", portFlag, p)
		}
	}

	switch r.Proto {
	case "", ProtoTCP, ProtoUDP:
	default:
		return errorx.IllegalArgument.New("invalid proto %q for rule %q: expected %q or %q", r.Proto, r.Name, ProtoTCP, ProtoUDP)
	}

	switch r.Name {
	case RuleBlocked:
		// The block list is a drop on three hooks, matching every protocol and
		// port. A port or protocol here would silently narrow it, which is the
		// opposite of what an operator adding a CIDR to a block list expects.
		if len(r.Ports) > 0 {
			return errorx.IllegalArgument.New("%q does not take ports: the block list drops every port and protocol", RuleBlocked)
		}
		if r.Proto != "" {
			return errorx.IllegalArgument.New("%q does not take proto: the block list drops every port and protocol", RuleBlocked)
		}
		if r.ICMPEcho {
			return errorx.IllegalArgument.New("%q does not take icmp_echo: the block list drops ICMP too", RuleBlocked)
		}
	case RuleMgmt:
		// mgmt renders a fixed ICMP type list that is strictly broader than an
		// echo-request accept, so icmp_echo could only mislead. Its transport
		// accept is TCP by construction (this is administrative access).
		//
		// Even proto=tcp is refused rather than accepted as a no-op: the value
		// would change nothing, cannot be expressed in the config schema (Block
		// carries no proto), and accepting it tells an operator their `set
		// --proto` landed when the renderer ignored it.
		if r.Proto != "" {
			return errorx.IllegalArgument.New("%q does not take proto: management access is TCP by construction", RuleMgmt)
		}
		if r.ICMPEcho {
			return errorx.IllegalArgument.New("%q does not take icmp_echo: management sources already receive the full ICMP type list", RuleMgmt)
		}
	case RuleInCluster:
		// A reserved block may be empty, which is how an operator disables it
		// without deleting it. It does carry a fixed shape though: the template
		// renders all three reserved blocks as TCP and gives them no echo accept,
		// so accepting either field here would silently ignore what was asked.
		if r.Proto != "" {
			return errorx.IllegalArgument.New("%q does not take proto: the in-cluster host-service ports are TCP by construction", RuleInCluster)
		}
		if r.ICMPEcho {
			return errorx.IllegalArgument.New("%q does not take icmp_echo: pod-to-host ICMP is not part of the host-service allowance", RuleInCluster)
		}
	default:
		// An allow rule is allowed to be incomplete. `create-allow-rule` declares
		// a rule before it has any members, and the element verbs populate it in
		// whatever order the operator runs them, so every intermediate state has
		// to be representable. An incomplete rule renders no nft rule at all —
		// the template gates each emission on the address and port sets being
		// non-empty — so it grants nothing rather than granting too much.
		// applyAndPersist warns about them; see Table.IncompleteAllowRules.
	}

	// Order the port list numerically here rather than in each mutator, so a rule
	// authored in a config file renders the same as one built up through the CLI.
	// Without this the rendered document would depend on the order the operator
	// happened to list ports in, and every re-render would churn the artifact.
	sortPortSpecs(r.Ports)

	return nil
}

// flagNames returns the CLI flag names to quote in a validation error for this
// rule, so the message points at the input the operator actually typed.
func (r *Rule) flagNames() (cidrFlag, portFlag string) {
	switch r.Name {
	case RuleMgmt:
		return "--mgmt-cidrs", "--mgmt-ports"
	case RuleBlocked:
		return "--blocked-cidrs", "--ports"
	case RuleInCluster:
		return "--pod-cidr", "--in-cluster-ports"
	default:
		return "--cidrs", "--ports"
	}
}

// AddCIDRs adds CIDRs to the rule, ignoring ones already present. The list is
// kept sorted so a render is stable regardless of the order entries arrived in.
func (r *Rule) AddCIDRs(cidrs []string) error {
	for _, c := range cidrs {
		if err := sanity.ValidateCIDR(c); err != nil {
			cidrFlag, _ := r.flagNames()
			return errorx.IllegalArgument.Wrap(err, "invalid %s %q", cidrFlag, c)
		}
		if !sanity.Contains(c, r.CIDRs) {
			r.CIDRs = append(r.CIDRs, c)
		}
	}
	sort.Strings(r.CIDRs)
	return nil
}

// RemoveCIDRs drops CIDRs from the rule. Removing an absent entry is a no-op.
func (r *Rule) RemoveCIDRs(cidrs []string) {
	r.CIDRs = without(r.CIDRs, cidrs)
}

// SetCIDRs atomically replaces the rule's full address list. An empty
// (non-nil) slice clears it.
func (r *Rule) SetCIDRs(cidrs []string) error {
	for _, c := range cidrs {
		if err := sanity.ValidateCIDR(c); err != nil {
			cidrFlag, _ := r.flagNames()
			return errorx.IllegalArgument.Wrap(err, "invalid %s %q", cidrFlag, c)
		}
	}
	r.CIDRs = sortedDedupe(cidrs)
	return nil
}

// AddPorts adds port specs to the rule, ignoring ones already present.
func (r *Rule) AddPorts(ports []string) error {
	for _, p := range ports {
		if err := validatePortSpec(p); err != nil {
			_, portFlag := r.flagNames()
			return errorx.IllegalArgument.Wrap(err, "invalid %s %q", portFlag, p)
		}
		if !sanity.Contains(p, r.Ports) {
			r.Ports = append(r.Ports, p)
		}
	}
	sortPortSpecs(r.Ports)
	return nil
}

// RemovePorts drops port specs from the rule. Removal is by exact spec, so
// removing "2379" from a rule holding "2379-2380" is a no-op rather than a
// partial range split — nft ranges are single set elements and splitting one
// silently would be a surprising way to change a firewall.
func (r *Rule) RemovePorts(ports []string) {
	r.Ports = without(r.Ports, ports)
}

// SetPorts atomically replaces the rule's full port list. An empty (non-nil)
// slice clears it.
func (r *Rule) SetPorts(ports []string) error {
	for _, p := range ports {
		if err := validatePortSpec(p); err != nil {
			_, portFlag := r.flagNames()
			return errorx.IllegalArgument.Wrap(err, "invalid %s %q", portFlag, p)
		}
	}
	out := dedupeStrings(ports)
	sortPortSpecs(out)
	r.Ports = out
	return nil
}

// validatePortSpec accepts a single port ("6443") or an inclusive range
// ("2379-2380"). Each endpoint goes through sanity.ValidatePort, so the range
// form gains no ground on what a bare port is allowed to be.
func validatePortSpec(s string) error {
	lo, hi, err := parsePortSpec(s)
	if err != nil {
		return err
	}
	if lo > hi {
		return errorx.IllegalArgument.New("port range %q is inverted: %d is above %d", s, lo, hi)
	}
	return nil
}

// parsePortSpec splits a port spec into its inclusive bounds. A single port
// yields lo == hi, so callers can order and compare both forms uniformly.
func parsePortSpec(s string) (lo, hi int, err error) {
	spec := strings.TrimSpace(s)
	loStr, hiStr, isRange := strings.Cut(spec, "-")
	if !isRange {
		if err := sanity.ValidatePort(spec); err != nil {
			return 0, 0, err
		}
		n, _ := strconv.Atoi(spec) // safe: ValidatePort already parsed and range-checked it
		return n, n, nil
	}
	if err := sanity.ValidatePort(loStr); err != nil {
		return 0, 0, errorx.IllegalArgument.Wrap(err, "invalid range start in %q", spec)
	}
	if err := sanity.ValidatePort(hiStr); err != nil {
		return 0, 0, errorx.IllegalArgument.Wrap(err, "invalid range end in %q", spec)
	}
	lo, _ = strconv.Atoi(loStr)
	hi, _ = strconv.Atoi(hiStr)
	return lo, hi, nil
}

// sortPortSpecs orders port specs numerically by their lower bound, so the
// rendered elements list reads in ascending port order rather than lexically
// (where "10250" would precede "6443"). Unparseable specs sort last; Validate
// rejects them before a render, so this only keeps the ordering total.
func sortPortSpecs(ports []string) {
	sort.SliceStable(ports, func(i, j int) bool {
		loI, hiI, errI := parsePortSpec(ports[i])
		loJ, hiJ, errJ := parsePortSpec(ports[j])
		if (errI == nil) != (errJ == nil) {
			return errI == nil
		}
		if errI != nil {
			return ports[i] < ports[j]
		}
		if loI != loJ {
			return loI < loJ
		}
		return hiI < hiJ
	})
}

// splitCIDRs partitions a validated mixed CIDR list into its IPv4 and IPv6
// members, preserving order. Validate has already run, so a value that somehow
// fails to classify is dropped from both lists rather than smuggled into the
// wrong-family nft set (which nft would reject at apply time anyway).
func splitCIDRs(cidrs []string) (v4, v6 []string) {
	for _, c := range cidrs {
		isV6, err := sanity.CIDRIsIPv6(c)
		if err != nil {
			continue
		}
		if isV6 {
			v6 = append(v6, c)
		} else {
			v4 = append(v4, c)
		}
	}
	return v4, v6
}

// without returns in with every element of drop removed, preserving order.
func without(in, drop []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !sanity.Contains(v, drop) {
			out = append(out, v)
		}
	}
	return out
}

func sortedDedupe(in []string) []string {
	out := dedupeStrings(in)
	sort.Strings(out)
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
