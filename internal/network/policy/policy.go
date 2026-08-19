// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// Action is the nft verdict a policy renders: classify-and-accept (stamp) or
// drop (deny). Every policy is exactly one of the two.
type Action string

const (
	// ActionStamp classifies matching packets into an HTB priority class
	// (`meta priority set <value> accept`).
	ActionStamp Action = "stamp"
	// ActionDeny drops matching packets, before the established/related
	// fast-path. A membership deny drops both directions; adding --ports narrows
	// it to a listener port and drops the request leg only (see renderDenyRules).
	ActionDeny Action = "deny"
)

// Direction selects which half of the forward chain a stamp rule renders into.
// It is empty for deny policies (whose direction follows from the match). For
// stamp policies it is not a caller-supplied value: Validate derives it from
// the --stamp class (every class in the mark map has exactly one direction),
// so it can never contradict the class it names.
type Direction string

const (
	// DirectionIngress renders into the peer→pod block (`ip daddr POD_CIDR …
	// tcp dport`).
	DirectionIngress Direction = "ingress"
	// DirectionEgress renders into the pod→peer block (`ip saddr POD_CIDR …
	// tcp sport`).
	DirectionEgress Direction = "egress"
)

// Policy is the static definition of one named category, mirroring the registry
// JSON schema. CIDR membership is deliberately NOT a field: it lives in the
// live nft set and is owned by the daemon poll loop, never persisted to the
// registry or the .nft file. The initial `--cidrs` membership supplied at
// create time is applied to the live kernel separately (see Manager.Create).
type Policy struct {
	Name            string    `json:"name"`
	Action          Action    `json:"action"`
	Stamp           string    `json:"stamp"`             // HTB class (from --stamp); "" for deny
	ReplyStamp      string    `json:"reply_stamp"`       // reply class (from --reply-stamp); "" if unset
	Direction       Direction `json:"direction"`         // derived from Stamp's class by Validate; "" for deny
	Ports           []string  `json:"ports"`             // static workload listener ports (from --ports); nil if none or ManagedPorts
	ManagedPorts    bool      `json:"managed_ports"`     // true when <name>_ports is filled by the daemon from statusz, not seeded here
	FromEntityWorld bool      `json:"from_entity_world"` // true if --from-entity world (no IP-set clause)
	CreatedAt       time.Time `json:"created_at"`        // tiebreaker within a tier, preserved across a --force replace
}

// Validate rejects any policy + initial-CIDR combination that would be unsafe
// or nonsensical to render. It is the single gate before the renderer; every
// untrusted token (name, class, ports, CIDRs) is checked so a malformed value
// can never break the atomic nft transaction or smuggle in nft syntax.
func (p *Policy) Validate(cidrs []string) error {
	if err := sanity.ValidateIdentifier(p.Name); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --name %q", p.Name)
	}

	switch p.Action {
	case ActionStamp:
		if err := p.validateStamp(); err != nil {
			return err
		}
	case ActionDeny:
		if err := p.validateDeny(); err != nil {
			return err
		}
	default:
		return errorx.IllegalArgument.New("policy must specify exactly one of --stamp or --deny")
	}

	if p.FromEntityWorld && len(cidrs) > 0 {
		return errorx.IllegalArgument.New("--from-entity world is mutually exclusive with --cidrs")
	}

	for _, port := range p.Ports {
		if err := sanity.ValidatePort(port); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --ports entry %q", port)
		}
	}

	if p.ManagedPorts {
		if p.Action != ActionStamp {
			return errorx.IllegalArgument.New("managed ports are only valid with --stamp")
		}
		if len(p.Ports) > 0 {
			return errorx.IllegalArgument.New("managed ports are mutually exclusive with static --ports (the daemon fills the set from statusz)")
		}
	}

	return p.validateCIDRs(cidrs)
}

// validateStamp resolves --stamp to its class and derives p.Direction from it
// (every class has exactly one direction, so there is no independent
// --direction flag to validate against). For --reply-stamp, the
// reply class must be the mirror direction of the forward class — e.g. an
// egress --stamp pairs only with an ingress --reply-stamp — since a reply is
// definitionally the reverse leg of the forward flow.
func (p *Policy) validateStamp() error {
	if p.Stamp == "" {
		return errorx.IllegalArgument.New("--stamp requires a class name")
	}
	c, err := lookupClass(p.Stamp)
	if err != nil {
		return err
	}
	p.Direction = c.Direction

	if p.ReplyStamp != "" {
		rc, err := lookupClass(p.ReplyStamp)
		if err != nil {
			return err
		}
		if c.Direction != DirectionEgress {
			return errorx.IllegalArgument.New(
				"--reply-stamp is only valid when --stamp resolves to an egress class (got %q)", p.Stamp)
		}
		if rc.Direction != DirectionIngress {
			return errorx.IllegalArgument.New(
				"--reply-stamp class %q must resolve to an ingress class (the mirror of --stamp %q)", p.ReplyStamp, p.Stamp)
		}
	}
	return nil
}

func (p *Policy) validateDeny() error {
	if p.Stamp != "" || p.ReplyStamp != "" {
		return errorx.IllegalArgument.New("--deny is mutually exclusive with --stamp and --reply-stamp")
	}
	if p.Direction != "" {
		return errorx.IllegalArgument.New("--direction does not apply to --deny (the direction follows from the match)")
	}
	if p.ManagedPorts {
		return errorx.IllegalArgument.New("managed ports do not apply to --deny")
	}
	// A deny needs at least one narrowing clause. With neither an IP set
	// (--from-entity world suppresses it) nor a port set, the rule renders as a
	// bare `drop` and takes down every forwarded packet on the node.
	if p.FromEntityWorld && len(p.Ports) == 0 {
		return errorx.IllegalArgument.New("--deny with --from-entity world requires --ports")
	}
	return nil
}

// validateCIDRs checks the initial membership entries against the set type the
// policy renders: compound ip:port keys for a --reply-stamp policy (matching an
// `ipv4_addr . inet_service` / `ipv6_addr . inet_service` set), plain CIDRs
// otherwise. Both address families are accepted; each entry is routed to the
// policy's v4 (@name) or v6 (@name6) set by family at render/apply time.
func (p *Policy) validateCIDRs(cidrs []string) error {
	for _, c := range cidrs {
		if p.isCompoundSet() {
			if err := validateIPPort(c); err != nil {
				return err
			}
			continue
		}
		if err := sanity.ValidateCIDR(c); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --cidrs entry %q", c)
		}
	}
	return nil
}

// V6SetName returns the IPv6 companion set name for a policy (or its compound
// set): the base policy name with a "6" suffix. The IPv4 set keeps the bare
// policy name so the on-disk registry and existing element tooling are
// unchanged; the v6 set is the parallel ipv6_addr(. inet_service) set the
// dual-stack renderer declares. The renderer, the Manager's apply/snapshot
// paths, and the traffic-shaper daemon all derive the v6 set name through this
// one function so render, diff, and apply can never disagree on the spelling.
func V6SetName(name string) string { return name + "6" }

// isCompoundSet reports whether the policy's nft set is a compound
// `ipv4_addr . inet_service` key set — true only for --reply-stamp policies,
// whose --cidrs entries are ip:port destination pairs.
func (p *Policy) isCompoundSet() bool {
	return p.ReplyStamp != ""
}

// hasCIDRSet reports whether the policy renders a named `@<name>` membership
// set. A --from-entity world stamp policy matches any source/dest and so
// renders no set.
func (p *Policy) hasCIDRSet() bool {
	return !p.FromEntityWorld
}

// hasPortsSet reports whether the policy renders a named `@<name>_ports`
// listener-port set — either statically seeded (len(Ports) > 0) or daemon-managed
// (ManagedPorts, filled from statusz at runtime and declared empty at render).
func (p *Policy) hasPortsSet() bool {
	return len(p.Ports) > 0 || p.ManagedPorts
}

func validateIPPort(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return errorx.IllegalArgument.New("invalid --cidrs entry %q: --reply-stamp policies require ip:port pairs (bracket IPv6 hosts, e.g. [2001:db8::1]:443)", s)
	}
	if ip := net.ParseIP(host); ip == nil {
		return errorx.IllegalArgument.New("invalid --cidrs entry %q: %q is not an IP address", s, host)
	}
	if err := sanity.ValidatePort(port); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --cidrs entry %q", s)
	}
	return nil
}

// elementToken renders one --cidrs entry into its nft set element token and
// reports its address family, so the caller can route it to the policy's IPv4
// (@name) or IPv6 (@name6) set. Compound (--reply-stamp) entries are ip:port
// pairs converted through CompoundElement; plain entries are CIDRs passed
// through as-is. Input is assumed already validated.
func elementToken(p *Policy, cidr string) (token string, isV6 bool, err error) {
	if p.isCompoundSet() {
		tok, err := CompoundElement(cidr)
		if err != nil {
			return "", false, err
		}
		host, _, splitErr := net.SplitHostPort(cidr)
		if splitErr != nil {
			return "", false, errorx.IllegalArgument.Wrap(splitErr, "invalid ip:port %q", cidr)
		}
		addr, addrErr := netip.ParseAddr(host)
		if addrErr != nil {
			return "", false, errorx.IllegalArgument.New("invalid ip:port %q: %q is not an IP address", cidr, host)
		}
		return tok, addr.Is6() && !addr.Is4In6(), nil
	}
	isV6, err = sanity.CIDRIsIPv6(cidr)
	if err != nil {
		return "", false, err
	}
	return cidr, isV6, nil
}

// setElementsByFamily converts initial --cidrs entries into nft element tokens,
// partitioned by address family so each lands in the policy's IPv4 (@name) or
// IPv6 (@name6) set. The input is assumed already validated, so a token
// conversion error (only possible on a malformed entry) cannot fire here; it
// shares CompoundElement so the CLI apply path and the daemon poll loop's diff
// render compound elements identically.
func setElementsByFamily(p *Policy, cidrs []string) (v4, v6 []string) {
	for _, c := range cidrs {
		tok, isV6, err := elementToken(p, c)
		if err != nil {
			continue
		}
		if isV6 {
			v6 = append(v6, tok)
		} else {
			v4 = append(v4, tok)
		}
	}
	return v4, v6
}

// portElements returns the --ports values joined for an nft `elements = { … }`
// clause.
func portElements(ports []string) string {
	return strings.Join(ports, ", ")
}

// PortsSetName returns the nft set name that holds a policy's listener ports
// (`<name>_ports`). Rendered by renderSetDecls, referenced by the stamp rule's
// `tcp dport/sport @<name>_ports` clause, and written by Manager.ApplyPorts.
func PortsSetName(policyName string) string {
	return policyName + "_ports"
}

// isSpecific reports whether p is a "specific" stamp policy for tier-3/4
// grouping and overlap purposes: an --stamp policy that is not --from-entity
// world (i.e. one that renders an IP-set clause). Deny policies and
// fallthrough stamp policies are never "specific".
func isSpecific(p *Policy) bool {
	return p.Action == ActionStamp && !p.FromEntityWorld
}

// portsKey returns a canonical, order-insensitive key for a --ports value, so
// two policies naming the same ports in a different flag order still compare
// equal for grouping/overlap purposes.
func portsKey(ports []string) string {
	sorted := append([]string(nil), ports...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// groupKey returns the (Direction, Ports) grouping key used both by
// renderChain's tier-3/4 ordering and by the overlap check below. A
// managed-ports policy carries no static ports — its real listener ports are
// reconciled from statusz at runtime and are distinct per policy by design — so
// each is keyed to its own group: two managed-ports policies can never be seen
// as overlapping statically (their empty static port lists would otherwise
// collide), and the runtime distinction they actually have is invisible here.
func groupKey(p *Policy) string {
	if p.ManagedPorts {
		return string(p.Direction) + "|managed:" + p.Name
	}
	return string(p.Direction) + "|" + portsKey(p.Ports)
}

// checkNoOverlap rejects candidate if it is a "specific" stamp policy that
// shares its (Direction, Ports) group with another existing specific policy.
// Two specific policies claiming the same traffic would have an ambiguous
// classification outcome, since set membership -- and therefore which
// policy's CIDR actually matches a given packet -- can change independently
// after create via the daemon/element verbs (Add/Remove/Set), so the check
// is on the group key alone, not on the initial --cidrs values. Fallthrough
// (--from-entity world) policies are exempt: they intentionally match
// anything, and creation order (see renderChain) already gives them a
// deterministic position.
//
// existing entries matching candidate.Name are skipped so a --force
// re-create of the same policy never self-rejects.
func checkNoOverlap(existing []*Policy, candidate *Policy) error {
	if !isSpecific(candidate) {
		return nil
	}
	for _, p := range existing {
		if p.Name == candidate.Name {
			continue
		}
		if isSpecific(p) && groupKey(p) == groupKey(candidate) {
			return errorx.IllegalArgument.New(
				"policy %q overlaps with existing policy %q: both are specific --stamp policies for direction=%s ports=%v",
				candidate.Name, p.Name, candidate.Direction, candidate.Ports)
		}
	}
	return nil
}
