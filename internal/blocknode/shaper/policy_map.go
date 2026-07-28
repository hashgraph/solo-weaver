// SPDX-License-Identifier: Apache-2.0

package shaper

import (
	"context"
	"sort"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/joomcode/errorx"
)

// Category is a statusz traffic category as reported by the block node's statusz
// endpoints (inbound-clients / outbound-clients). The vocabulary is fixed by the
// BN's contract, not operator-configurable: partner, publisher, public,
// restricted.
type Category string

const (
	// CategoryPublisher is the publisher category.
	CategoryPublisher Category = "publisher"
	// CategoryPartner is the partner category. It appears on both statusz
	// endpoints and binds to a different policy set per direction: inbound
	// partner clients feed bn-partner-out, while outbound partner connections are
	// the peer block nodes this BN backfills from and feed the compound
	// bn-backfill set.
	CategoryPartner Category = "partner"
	// CategoryRestricted is the restricted category.
	CategoryRestricted Category = "restricted"
	// CategoryPublic is the public category. It is recognized but deliberately
	// left unmapped: public access is enforced by the bn-public-out port-match
	// set, not reconciled from statusz membership, so a public endpoint is
	// neither an "unknown category" surprise nor a monitor-owned set.
	CategoryPublic Category = "public"
)

// Direction is the statusz endpoint an endpoint set is read from: inbound
// (clients connecting to this BN) or outbound (connections this BN opens). The
// same category string can bind to a different policy set per direction, so a
// binding is keyed on (direction, category), not category alone.
type Direction int

const (
	// Inbound is the statusz/inbound-clients endpoint.
	Inbound Direction = iota
	// Outbound is the statusz/outbound-clients endpoint.
	Outbound
)

// bindingKey identifies an owned policy set by the (direction, category) it is
// fed from. Category alone is not a unique key: partner appears under both
// directions bound to different sets.
type bindingKey struct {
	dir Direction
	cat Category
}

// categoryBinding is the fixed mapping from a (direction, category) to the nft
// policy set it drives and how that set is keyed.
type categoryBinding struct {
	// policyName is the nft set (network policy) the endpoints populate.
	policyName string
	// compound is true when the set is a compound `ipv4_addr . inet_service`
	// key set fed ip:port endpoints (bn-backfill); false for plain ipv4_addr
	// sets fed host/CIDR endpoints.
	compound bool
}

// categoryBindings is the internal, non-configurable (direction, category) →
// policy mapping. Both the BN's category vocabulary and the provisioner's policy
// names are fixed in code, so this is a static table rather than config. Keys
// not listed here — the public category, or the operator-curated mgmt sets — are
// never touched by the monitor.
//
// bn-backfill is fed by OUTBOUND partner endpoints (the peer block nodes this BN
// backfills from), keyed on the compound destination address AND port. Inbound
// partner clients are a distinct set (bn-partner-out).
var categoryBindings = map[bindingKey]categoryBinding{
	{Inbound, CategoryPublisher}:  {policyName: "bn-publisher"},
	{Inbound, CategoryPartner}:    {policyName: "bn-partner-out"},
	{Inbound, CategoryRestricted}: {policyName: "bn-restricted"},
	{Outbound, CategoryPartner}:   {policyName: "bn-backfill", compound: true},
}

// categoryEndpoints is one poll's desired membership view, keyed by the
// (direction, category) each endpoint set was read from. For each key PRESENT in
// the map, the slice is its active endpoints: plain IPv4 hosts/CIDRs for the
// plain sets, "ip:port" pairs for the compound bn-backfill set. The
// present-vs-absent distinction is load bearing and preserved end to end:
//
//   - a key present with an empty (or nil) slice clears that policy's set;
//   - a key absent from the map leaves that policy's set untouched.
//
// This is the minimal shape the diff needs.
type categoryEndpoints map[bindingKey][]string

// PolicyDelta is the computed membership change for one policy set: the policy
// (nft set) name and the canonicalized add/delete lists.
type PolicyDelta struct {
	Policy string
	policy.SetDelta
}

// elementLister reads one nft set's live membership. Satisfied by policy.Runner
// (via ListElements) and by test fakes; kept deliberately narrow so the diff can
// never mutate sets — applying deltas is the apply path's responsibility, not
// this stage's.
type elementLister interface {
	ListElements(ctx context.Context, set string) ([]string, error)
}

// computePolicyDeltas maps each key present in ce to its policy, builds the
// desired membership, reads the policy's live nft set membership back from the
// kernel, and diffs the two. Keys absent from ce produce no delta (the set is
// left untouched); a present-but-empty key produces a delta that clears the set.
// Unmapped keys are ignored. No-op deltas are omitted and the result is ordered
// by policy name for deterministic output.
func computePolicyDeltas(ctx context.Context, lister elementLister, ce categoryEndpoints) ([]PolicyDelta, error) {
	keys := sortedKeys(ce)

	var deltas []PolicyDelta
	for _, k := range keys {
		b, ok := categoryBindings[k]
		if !ok {
			// Unmapped key (e.g. an mgmt set, or one the BN adds later): not
			// monitor-owned, so leave it untouched.
			continue
		}
		desired, err := desiredElements(b, ce[k])
		if err != nil {
			return nil, err
		}
		live, err := lister.ListElements(ctx, b.policyName)
		if err != nil {
			return nil, errorx.Decorate(err, "read live membership for policy %s", b.policyName)
		}
		delta := policy.DiffElements(desired, live)
		if delta.Empty() {
			continue
		}
		deltas = append(deltas, PolicyDelta{Policy: b.policyName, SetDelta: delta})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Policy < deltas[j].Policy })
	return deltas, nil
}

// canonicalDesiredMembership maps each owned key present in ce to its nft policy
// name and the canonical, nft-rendered form of its desired membership: compound
// "<ip> . <port>" tokens for the compound bn-backfill set, /32-collapsed and
// numerically ordered elements otherwise (via policy.CanonicalizeElements). This
// is exactly the rendering computePolicyDeltas diffs against live nft state, so
// digesting this form — rather than the raw statusz endpoints desiredMembership
// carries — means the digest only changes when the actual applied membership
// would change, regardless of how statusz happens to spell an equivalent
// host/CIDR across polls.
func canonicalDesiredMembership(ce categoryEndpoints) (map[string][]string, error) {
	m := make(map[string][]string, len(ce))
	for k, endpoints := range ce {
		b, ok := categoryBindings[k]
		if !ok {
			continue
		}
		elems, err := desiredElements(b, endpoints)
		if err != nil {
			return nil, err
		}
		m[b.policyName] = policy.CanonicalizeElements(elems)
	}
	return m, nil
}

// desiredElements converts a binding's raw statusz endpoints into nft set
// element tokens for its policy: compound "<ip> . <port>" keys for a compound
// set, plain elements otherwise. Canonicalization (ordering, /32 collapse)
// happens later in policy.DiffElements, so this only handles the compound-key
// conversion.
func desiredElements(b categoryBinding, endpoints []string) ([]string, error) {
	if !b.compound {
		return endpoints, nil
	}
	out := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		tok, err := policy.CompoundElement(e)
		if err != nil {
			return nil, errorx.Decorate(err, "policy %s endpoint", b.policyName)
		}
		out = append(out, tok)
	}
	return out, nil
}

// sortedKeys returns ce's keys ordered by (direction, category) for
// deterministic iteration.
func sortedKeys(ce categoryEndpoints) []bindingKey {
	keys := make([]bindingKey, 0, len(ce))
	for k := range ce {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dir != keys[j].dir {
			return keys[i].dir < keys[j].dir
		}
		return keys[i].cat < keys[j].cat
	})
	return keys
}
