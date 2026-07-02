// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"net"
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
	// ActionDeny drops matching packets in both directions, before the
	// established/related fast-path.
	ActionDeny Action = "deny"
)

// Direction selects which half of the forward chain a stamp rule renders into.
// It is empty for deny policies (which always apply to both directions). For
// stamp policies it is not a caller-supplied value: Validate derives it from
// the --stamp class (every class in the §5 mark map has exactly one
// direction), so it can never contradict the class it names.
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
// JSON schema (design §8.4.7). CIDR membership is deliberately NOT a field: it
// lives in the live nft set and is owned by the daemon poll loop, never
// persisted to the registry or the .nft file (§8.3.1). The initial `--cidrs`
// membership supplied at create time is applied to the live kernel separately
// (see Manager.Create).
type Policy struct {
	Name            string    `json:"name"`
	Action          Action    `json:"action"`
	Stamp           string    `json:"stamp"`             // HTB class (from --stamp); "" for deny
	ReplyStamp      string    `json:"reply_stamp"`       // reply class (from --reply-stamp); "" if unset
	Direction       Direction `json:"direction"`         // derived from Stamp's class by Validate; "" for deny
	Ports           []string  `json:"ports"`             // workload listener ports (from --ports); nil if none
	FromEntityWorld bool      `json:"from_entity_world"` // true if --from-entity world (no IP-set clause)
	CreatedAt       time.Time `json:"created_at"`        // tiebreaker within a tier, preserved across a --force replace
}

// Validate rejects any policy + initial-CIDR combination that would be unsafe
// or nonsensical to render, per the flag-validity rules in design §8.4.2 /
// §8.4.6. It is the single gate before the renderer; every untrusted token
// (name, class, ports, CIDRs) is checked so a malformed value can never break
// the atomic nft transaction or smuggle in nft syntax.
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

	if p.FromEntityWorld {
		if p.Action != ActionStamp {
			return errorx.IllegalArgument.New("--from-entity world is only valid with --stamp")
		}
		if len(cidrs) > 0 {
			return errorx.IllegalArgument.New("--from-entity world is mutually exclusive with --cidrs")
		}
	}

	for _, port := range p.Ports {
		if err := sanity.ValidatePort(port); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --ports entry %q", port)
		}
	}

	return p.validateCIDRs(cidrs)
}

// validateStamp resolves --stamp to its class and derives p.Direction from it
// (design §5: every class has exactly one direction, so there is no
// independent --direction flag to validate against). For --reply-stamp, the
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
		return errorx.IllegalArgument.New("--direction does not apply to --deny (it drops both directions)")
	}
	if len(p.Ports) > 0 {
		return errorx.IllegalArgument.New("--ports does not apply to --deny")
	}
	if p.FromEntityWorld {
		return errorx.IllegalArgument.New("--from-entity world does not apply to --deny")
	}
	return nil
}

// validateCIDRs checks the initial membership entries against the set type the
// policy renders: compound ip:port keys for a --reply-stamp policy (matching a
// `ipv4_addr . inet_service` set), plain IPv4 CIDRs otherwise.
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
		if ip, _, _ := net.ParseCIDR(c); ip.To4() == nil {
			return errorx.IllegalArgument.New(
				"invalid --cidrs entry %q: IPv6 is not yet supported; the inet weaver sets use ipv4_addr", c)
		}
	}
	return nil
}

// isCompoundSet reports whether the policy's nft set is a compound
// `ipv4_addr . inet_service` key set — true only for --reply-stamp policies,
// whose --cidrs entries are ip:port destination pairs (design §8.4.6).
func (p *Policy) isCompoundSet() bool {
	return p.ReplyStamp != ""
}

// hasCIDRSet reports whether the policy renders a named `@<name>` membership
// set. A --from-entity world stamp policy matches any source/dest and so
// renders no set.
func (p *Policy) hasCIDRSet() bool {
	return !p.FromEntityWorld
}

func validateIPPort(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return errorx.IllegalArgument.New("invalid --cidrs entry %q: --reply-stamp policies require ip:port pairs", s)
	}
	if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
		return errorx.IllegalArgument.New("invalid --cidrs entry %q: %q is not an IPv4 address", s, host)
	}
	if err := sanity.ValidatePort(port); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --cidrs entry %q", s)
	}
	return nil
}

// setElements converts initial --cidrs entries into nft element tokens for the
// policy's set: `<ip> . <port>` compound keys for --reply-stamp policies, plain
// CIDRs otherwise. The input is assumed already validated.
func setElements(p *Policy, cidrs []string) []string {
	if !p.isCompoundSet() {
		return cidrs
	}
	out := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		host, port, _ := net.SplitHostPort(c)
		out = append(out, host+" . "+port)
	}
	return out
}

// portElements returns the --ports values joined for an nft `elements = { … }`
// clause.
func portElements(ports []string) string {
	return strings.Join(ports, ", ")
}
