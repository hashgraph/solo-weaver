// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"net/netip"
	"sort"

	"github.com/joomcode/errorx"
)

// The workload-policy address sets are declared `flags interval` (render.go,
// renderSetDecls) so they can hold CIDRs rather than only single hosts. nft
// refuses two elements of such a set where one contains the other:
//
//	Error: conflicting intervals specified
//	        set bn-publisher { type ipv4_addr; flags interval;
//	                           elements = { 10.0.0.0/24, 10.0.0.5/32 }; }
//
// Containment is the ONLY conflict nft raises here, and it is the only way two
// CIDR prefixes can overlap at all: prefixes form a tree, so any two are either
// disjoint or one strictly contains the other -- partial overlap is impossible.
// Two consequences this file relies on, both verified against nftables v1.0.6:
//
//   - Adjacent prefixes are accepted as-is (10.0.0.0/25 + 10.0.0.128/25 coexist
//     as two elements). Merging adjacency is what `auto-merge` adds, and it is
//     not needed to avoid the error -- so we do not do interval arithmetic and
//     never emit a prefix the caller did not supply.
//   - An exact duplicate is accepted (nft folds it silently), so only STRICT
//     containment is a conflict. Re-adding a CIDR that is already a member stays
//     the no-op it is today.
//
// The policy plane deliberately does not turn on `auto-merge` (see the note in
// renderSetDecls); these helpers keep the kernel's stored membership equal to
// what we sent, so ListElements round-trips it exactly.

// cidrElem is one nft address-set element token parsed into prefix form,
// carrying the caller's original spelling for error messages.
type cidrElem struct {
	raw string       // the caller's spelling, echoed back in errors
	pfx netip.Prefix // masked prefix form; a bare host becomes a full-length prefix
	idx int          // position in the input, so a filtered result keeps input order
}

// parseCIDRElems converts address-set element tokens into prefix form, dropping
// the tokens that carry no interval semantics and so can never conflict:
// compound `<ip> . <port>` keys (their sets are declared without `flags
// interval`) and anything that does not parse as an address or prefix. Dropped
// tokens are invisible to the containment helpers, which therefore never reject
// or prune them.
func parseCIDRElems(elements []string) []cidrElem {
	out := make([]cidrElem, 0, len(elements))
	for i, e := range elements {
		k := parseElement(e)
		if !k.parsed || k.port >= 0 {
			continue
		}
		out = append(out, cidrElem{raw: e, pfx: netip.PrefixFrom(k.addr, k.bits), idx: i})
	}
	return out
}

// sortCIDRElems orders elements by address then prefix length, which places a
// covering prefix immediately before every prefix it covers. Both containment
// helpers walk the result and compare each candidate against only the most
// recently retained element, which is sufficient: if some earlier retained
// element R covered the candidate C, then every element sorted between them --
// including the last retained one, L -- has an address inside R's range, and a
// CIDR prefix whose address falls strictly inside R is necessarily a subprefix
// of R (prefixes are aligned to their own length). L would therefore have been
// covered by R and pruned rather than retained, so the nearest ancestor of C is
// always the last element still standing.
func sortCIDRElems(elems []cidrElem) {
	sort.Slice(elems, func(i, j int) bool {
		if c := elems[i].pfx.Addr().Compare(elems[j].pfx.Addr()); c != 0 {
			return c < 0
		}
		return elems[i].pfx.Bits() < elems[j].pfx.Bits()
	})
}

// covers reports whether outer STRICTLY contains inner. Equal prefixes are not
// a containment (nft accepts an exact duplicate), and a prefix never covers
// itself. Mixed families never satisfy this: netip.Prefix.Contains is false for
// an address of the other family.
func covers(outer, inner netip.Prefix) bool {
	return outer.Bits() < inner.Bits() && outer.Contains(inner.Addr())
}

// containmentPair returns the first pair of element tokens where one strictly
// contains the other -- the membership nft would reject with "conflicting
// intervals specified". outer is the covering prefix, inner the covered one,
// both in the caller's original spelling so an error message can name what was
// actually typed. found is false when the list is conflict-free.
//
// Used by the operator-authored paths (`network policy create`/`add`/`set`),
// which reject rather than fold: folding would leave a later
// `network policy remove --cidr <covered>` with no correct answer, because
// policy set membership is never persisted and the kernel is the only copy of
// what was authored.
func containmentPair(elements []string) (outer, inner string, found bool) {
	elems := parseCIDRElems(elements)
	if len(elems) < 2 {
		return "", "", false
	}
	sortCIDRElems(elems)
	last := elems[0]
	for _, cur := range elems[1:] {
		if covers(last.pfx, cur.pfx) {
			return last.raw, cur.raw, true
		}
		last = cur
	}
	return "", "", false
}

// rejectContainment reports a containment conflict as an operator-facing error,
// naming both prefixes in the spelling they were given and saying what to do
// about it. candidates is the membership being written; live, when non-nil, is
// the membership already in the kernel (supplied only by the incremental
// `network policy add` path, where a candidate can collide with a stored member
// rather than with another candidate). Returns nil when there is no conflict.
func rejectContainment(policyName string, candidates, live []string) error {
	if outer, inner, found := containmentPair(candidates); found {
		return errorx.IllegalArgument.New(
			"policy %q cannot hold both %s and %s: %s already covers %s, and an nft address set rejects overlapping entries. "+
				"Drop %s -- %s already matches every address in it.",
			policyName, outer, inner, outer, inner, inner, outer)
	}
	if len(live) == 0 {
		return nil
	}
	// candidates is known conflict-free by the check above, and live cannot
	// conflict with itself (nft would never have stored it), so any conflict in
	// the combined list is between a candidate and a stored member.
	outer, inner, found := containmentPair(append(append([]string{}, live...), candidates...))
	if !found {
		return nil
	}
	if isLiveElement(outer, live) {
		return errorx.IllegalArgument.New(
			"policy %q already permits %s through its existing member %s: an nft address set rejects overlapping entries, "+
				"and %s already matches every address in %s, so nothing needs to be added. To narrow the policy, remove %s first.",
			policyName, inner, outer, outer, inner, outer)
	}
	return errorx.IllegalArgument.New(
		"policy %q cannot take %s: it covers the existing member %s, and an nft address set rejects overlapping entries. "+
			"Remove %s first if you meant to widen the policy to %s.",
		policyName, outer, inner, inner, outer)
}

// rejectMissingMembers reports an attempt to remove something the set does not
// hold, before nft is asked to delete it. `nft delete element` names exact
// elements and errors with a bare "Error: element does not exist" for anything
// absent -- and because the transaction is atomic, one absent entry in a batch
// silently removes NONE of the others. Both are checked here so the operator
// learns which entry was wrong and what to do about it.
//
// tokens are nft element tokens (already converted from the operator's --cidr
// values, so a compound policy's ip:port pairs are in `<ip> . <port>` form).
// live is the set's current membership as the kernel spells it.
func rejectMissingMembers(policyName string, tokens, live []string) error {
	liveCanon := make(map[string]struct{}, len(live))
	for _, l := range live {
		liveCanon[parseElement(l).canon] = struct{}{}
	}
	for _, tok := range tokens {
		if _, ok := liveCanon[parseElement(tok).canon]; ok {
			continue
		}
		// The covered case is the confusing one: the policy really does permit
		// every address in tok (via the covering member), so a bare "does not
		// exist" reads like a bug. Name the member that is standing in its way.
		if outer, found := coveringMember(tok, live); found {
			return errorx.IllegalArgument.New(
				"%s is not a member of policy %q: it is covered by %s, which nft stores as a single element. "+
					"Removing part of a member is not supported -- remove %s and re-add the ranges you want to keep.",
				tok, policyName, outer, outer)
		}
		return errorx.IllegalArgument.New(
			"%s is not a member of policy %q, so nothing was removed. "+
				"Run `solo-provisioner network policy show --name %s` to list current membership.",
			tok, policyName, policyName)
	}
	return nil
}

// coveringMember returns the live element that strictly contains tok, if any.
// Compound and unparseable tokens have no containment relation and never match.
func coveringMember(tok string, live []string) (string, bool) {
	cand := parseCIDRElems([]string{tok})
	if len(cand) == 0 {
		return "", false
	}
	for _, member := range parseCIDRElems(live) {
		if covers(member.pfx, cand[0].pfx) {
			return member.raw, true
		}
	}
	return "", false
}

// isLiveElement reports whether tok names the same set element as one of live,
// comparing canonical forms so a candidate's "10.0.0.5/32" matches the bare
// "10.0.0.5" the kernel prints for the same element.
func isLiveElement(tok string, live []string) bool {
	canon := parseElement(tok).canon
	for _, l := range live {
		if parseElement(l).canon == canon {
			return true
		}
	}
	return false
}

// PruneContainedCIDRs returns elements with every token strictly contained by another
// retained token removed. The result is always a SUBSET of the input in input
// order -- no prefix is rewritten, merged, or invented -- so the kernel stores
// exactly the spellings we sent, ListElements round-trips them, and
// DiffElements sees the same canonical forms it would have seen anyway.
// Dropping the covered prefix is semantically identical to merging it: the
// covering member already matches every address in it.
//
// Used by the derived path (the traffic-shaper daemon's ApplyMembership /
// ApplySets), where membership is recomputed from upstream and fully replaced
// every tick and individual elements are never deleted, so there is no authored
// granularity to lose. Rejecting there would wedge the reconcile on an upstream
// quirk and leave a restrict-policy set empty -- a fail-open.
func PruneContainedCIDRs(elements []string) []string {
	elems := parseCIDRElems(elements)
	if len(elems) < 2 {
		return elements
	}
	sortCIDRElems(elems)
	pruned := make(map[int]struct{})
	last := elems[0]
	for _, cur := range elems[1:] {
		if covers(last.pfx, cur.pfx) {
			pruned[cur.idx] = struct{}{}
			continue
		}
		last = cur
	}
	if len(pruned) == 0 {
		return elements
	}
	out := make([]string, 0, len(elements)-len(pruned))
	for i, e := range elements {
		if _, drop := pruned[i]; drop {
			continue
		}
		out = append(out, e)
	}
	return out
}
