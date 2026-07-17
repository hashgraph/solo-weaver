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
// BN's contract, not operator-configurable.
type Category string

const (
	// CategoryPublisher is the inbound publisher category.
	CategoryPublisher Category = "publisher"
	// CategoryPartner is the inbound partner category.
	CategoryPartner Category = "partner"
	// CategoryRestricted is the inbound restricted category.
	CategoryRestricted Category = "restricted"
	// CategoryPeerBN is the outbound peer-block-node category. Its endpoints are
	// compound ip:port pairs — the backfill set is keyed on destination address
	// AND port.
	CategoryPeerBN Category = "peer_bn"
)

// categoryBinding is the fixed mapping from a statusz category to the nft policy
// set it drives and how that set is keyed.
type categoryBinding struct {
	// policyName is the nft set (network policy) the category's endpoints
	// populate.
	policyName string
	// compound is true when the set is a compound `ipv4_addr . inet_service`
	// key set fed ip:port endpoints (bn-backfill); false for plain ipv4_addr
	// sets fed host/CIDR endpoints.
	compound bool
}

// categoryBindings is the internal, non-configurable statusz-category → policy
// mapping. Both the BN's category vocabulary and the provisioner's policy names
// are fixed in code, so this is a static table rather than config. Categories
// not listed here — including the operator-curated mgmt sets — are never touched
// by the monitor.
var categoryBindings = map[Category]categoryBinding{
	CategoryPublisher:  {policyName: "bn-publisher"},
	CategoryPartner:    {policyName: "bn-partner"},
	CategoryRestricted: {policyName: "bn-restricted"},
	CategoryPeerBN:     {policyName: "bn-backfill", compound: true},
}

// CategoryEndpoints is one poll's desired membership view, keyed by statusz
// category. For each category PRESENT in the map, the slice is its active
// endpoints: plain IPv4 hosts/CIDRs for inbound categories, "ip:port" pairs for
// the outbound peer_bn category. The present-vs-absent distinction is load
// bearing and preserved end to end:
//
//   - a category present with an empty (or nil) slice clears that policy's set;
//   - a category absent from the map leaves that policy's set untouched.
//
// This is the minimal shape the diff needs; the statusz client (#751) decodes the
// wire NetworkData response into it, and the apply path (#754) consumes the
// resulting deltas.
type CategoryEndpoints map[Category][]string

// PolicyDelta is the computed membership change for one policy set: the policy
// (nft set) name and the canonicalized add/delete lists.
type PolicyDelta struct {
	Policy string
	policy.SetDelta
}

// elementLister reads one nft set's live membership. Satisfied by policy.Runner
// (via ListElements) and by test fakes; kept deliberately narrow so the diff can
// never mutate sets — applying deltas is #754's responsibility, not this stage's.
type elementLister interface {
	ListElements(ctx context.Context, set string) ([]string, error)
}

// computePolicyDeltas maps each category present in ce to its policy, builds the
// desired membership, reads the policy's live nft set membership back from the
// kernel, and diffs the two. Categories absent from ce produce no delta (the set
// is left untouched); a present-but-empty category produces a delta that clears
// the set. Unmapped categories are ignored. No-op deltas are omitted and the
// result is ordered by policy name for deterministic output.
func computePolicyDeltas(ctx context.Context, lister elementLister, ce CategoryEndpoints) ([]PolicyDelta, error) {
	cats := make([]Category, 0, len(ce))
	for c := range ce {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })

	var deltas []PolicyDelta
	for _, c := range cats {
		b, ok := categoryBindings[c]
		if !ok {
			// Unmapped category (e.g. an mgmt set, or one the BN adds later):
			// not monitor-owned, so leave it untouched.
			continue
		}
		desired, err := desiredElements(b, ce[c])
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

// desiredElements converts a category's raw statusz endpoints into nft set
// element tokens for its policy: compound "<ip> . <port>" keys for the peer_bn
// category, plain elements otherwise. Canonicalization (ordering, /32 collapse)
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
