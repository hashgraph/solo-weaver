// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- containmentPair ---

func TestContainmentPair(t *testing.T) {
	tests := []struct {
		name          string
		elements      []string
		wantFound     bool
		wantOuter     string
		wantInner     string
		wantWhyNoConf string
	}{
		{
			name:          "disjoint v4 prefixes",
			elements:      []string{"10.0.0.0/24", "10.0.2.0/24"},
			wantWhyNoConf: "neither contains the other",
		},
		{
			name:          "adjacent v4 prefixes",
			elements:      []string{"10.0.0.0/25", "10.0.0.128/25"},
			wantWhyNoConf: "nft accepts adjacency without auto-merge, so we must not treat it as a conflict",
		},
		{
			name:          "exact duplicate",
			elements:      []string{"10.0.0.0/24", "10.0.0.0/24"},
			wantWhyNoConf: "nft folds an exact duplicate silently; only strict containment conflicts",
		},
		{
			name:          "duplicate spelled two ways",
			elements:      []string{"10.0.0.5/32", "10.0.0.5"},
			wantWhyNoConf: "a /32 and the bare host are the same element",
		},
		{
			name:      "strict containment /24 over /32",
			elements:  []string{"10.0.0.0/24", "10.0.0.5/32"},
			wantFound: true, wantOuter: "10.0.0.0/24", wantInner: "10.0.0.5/32",
		},
		{
			name:      "strict containment reported regardless of input order",
			elements:  []string{"10.0.0.5/32", "10.0.0.0/24"},
			wantFound: true, wantOuter: "10.0.0.0/24", wantInner: "10.0.0.5/32",
		},
		{
			name:      "strict containment /16 over /24",
			elements:  []string{"10.0.5.0/24", "10.0.0.0/16"},
			wantFound: true, wantOuter: "10.0.0.0/16", wantInner: "10.0.5.0/24",
		},
		{
			name:      "bare host covered by a prefix",
			elements:  []string{"10.0.0.0/24", "10.0.0.5"},
			wantFound: true, wantOuter: "10.0.0.0/24", wantInner: "10.0.0.5",
		},
		{
			name:      "v6 strict containment",
			elements:  []string{"2001:db8::/32", "2001:db8:1::/48"},
			wantFound: true, wantOuter: "2001:db8::/32", wantInner: "2001:db8:1::/48",
		},
		{
			// A v4 entry sorted before the conflicting v6 pair must not mask it:
			// the walk has to keep comparing within each family.
			name:      "v6 conflict is still found alongside an unrelated v4 entry",
			elements:  []string{"10.0.0.0/24", "2001:db8::/32", "2001:db8:1::/48"},
			wantFound: true, wantOuter: "2001:db8::/32", wantInner: "2001:db8:1::/48",
		},
		{
			name:          "mixed families, only one of which is conflict-free",
			elements:      []string{"10.0.0.0/24", "2001:db8::/32"},
			wantWhyNoConf: "containment can never span address families",
		},
		{
			name:          "compound ip . port elements are ignored",
			elements:      []string{"10.0.0.0 . 443", "10.0.0.5 . 443"},
			wantWhyNoConf: "compound sets carry no flags interval and cannot conflict (AC#4)",
		},
		{
			name:          "single element",
			elements:      []string{"10.0.0.0/24"},
			wantWhyNoConf: "nothing to compare against",
		},
		{
			name:          "empty",
			elements:      nil,
			wantWhyNoConf: "nothing to compare against",
		},
		{
			name:          "unparseable tokens are ignored",
			elements:      []string{"not-a-cidr", "also/nonsense"},
			wantWhyNoConf: "unparseable tokens carry no interval semantics",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outer, inner, found := containmentPair(tc.elements)
			if !tc.wantFound {
				require.False(t, found, "expected no conflict: %s", tc.wantWhyNoConf)
				require.Empty(t, outer)
				require.Empty(t, inner)
				return
			}
			require.True(t, found)
			// Both spellings come back exactly as supplied, so an error message
			// can echo what the operator actually typed.
			require.Equal(t, tc.wantOuter, outer, "covering prefix")
			require.Equal(t, tc.wantInner, inner, "covered prefix")
		})
	}
}

// --- PruneContainedCIDRs ---

func TestPruneContained(t *testing.T) {
	tests := []struct {
		name     string
		elements []string
		want     []string
		why      string
	}{
		{
			name:     "drops the covered prefix",
			elements: []string{"10.0.0.0/24", "10.0.0.5/32"},
			want:     []string{"10.0.0.0/24"},
			why:      "the covering member already matches every address in the covered one",
		},
		{
			name:     "keeps input order when pruning",
			elements: []string{"10.9.0.0/24", "10.0.0.5/32", "10.0.0.0/24"},
			want:     []string{"10.9.0.0/24", "10.0.0.0/24"},
			why:      "the result is a subset in input order, never re-sorted",
		},
		{
			name:     "prunes a whole nested chain down to the outermost",
			elements: []string{"10.0.0.0/8", "10.0.0.0/16", "10.0.0.0/24", "10.0.0.1"},
			want:     []string{"10.0.0.0/8"},
			why:      "every deeper prefix is covered by the /8",
		},
		{
			name:     "adjacency is preserved, not merged",
			elements: []string{"10.0.0.0/25", "10.0.0.128/25"},
			want:     []string{"10.0.0.0/25", "10.0.0.128/25"},
			why:      "nft accepts adjacency; merging would emit a /24 nobody supplied",
		},
		{
			name:     "already-minimal input is returned unchanged",
			elements: []string{"10.0.0.0/24", "10.0.2.0/24", "2001:db8::/32"},
			want:     []string{"10.0.0.0/24", "10.0.2.0/24", "2001:db8::/32"},
			why:      "nothing to prune",
		},
		{
			name:     "families are pruned independently",
			elements: []string{"10.0.0.0/24", "10.0.0.5/32", "2001:db8::/32", "2001:db8:1::/48"},
			want:     []string{"10.0.0.0/24", "2001:db8::/32"},
			why:      "each family's covered entry goes, neither affects the other",
		},
		{
			name:     "exact duplicates are left alone",
			elements: []string{"10.0.0.0/24", "10.0.0.0/24"},
			want:     []string{"10.0.0.0/24", "10.0.0.0/24"},
			why:      "nft accepts duplicates; pruning them is not our job",
		},
		{
			name:     "compound elements pass through untouched",
			elements: []string{"10.0.0.0 . 443", "10.0.0.5 . 443"},
			want:     []string{"10.0.0.0 . 443", "10.0.0.5 . 443"},
			why:      "compound sets have no interval semantics (AC#4)",
		},
		{
			name:     "unparseable tokens pass through untouched",
			elements: []string{"10.0.0.0/24", "garbage"},
			want:     []string{"10.0.0.0/24", "garbage"},
			why:      "an unexpected token must surface, not be silently dropped",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PruneContainedCIDRs(tc.elements)
			require.Equal(t, tc.want, got, tc.why)
			// The result is always a subset of the input: pruning never
			// rewrites, merges, or invents a prefix. This is what keeps
			// ListElements round-tripping exactly (AC#2).
			for _, e := range got {
				require.Contains(t, tc.elements, e, "pruning emitted a prefix that was not supplied")
			}
		})
	}
}

// --- Manager.Add ---

func TestAdd_RejectsCIDRCoveredByAnotherSuppliedCIDR(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	err := m.Add(context.Background(), "bn-publisher", []string{"10.0.0.0/24", "10.0.0.5/32"})
	require.Error(t, err)
	// Both prefixes are named, so the operator can see which pair collided (AC#1).
	require.Contains(t, err.Error(), "10.0.0.0/24")
	require.Contains(t, err.Error(), "10.0.0.5/32")
	// Nothing is written: a rejected add must not land half its entries.
	require.Empty(t, r.elements["bn-publisher"], "a rejected add wrote to the live set")
}

func TestAdd_RejectsCIDRCoveredByExistingMember(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"10.0.0.0/24"}))

	err := m.Add(context.Background(), "bn-publisher", []string{"10.0.0.5/32"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "10.0.0.0/24")
	require.Contains(t, err.Error(), "10.0.0.5/32")
	// The message tells the operator no action is needed, since the covering
	// member already matches the address they tried to add.
	require.Contains(t, err.Error(), "already permits")
	require.Equal(t, []string{"10.0.0.0/24"}, r.elements["bn-publisher"], "live membership changed")
}

func TestAdd_RejectsCIDRCoveringAnExistingMember(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"10.0.0.5/32"}))

	// The reverse direction is equally a conflict for nft, and the message has to
	// point at the narrower member as the thing to remove first.
	err := m.Add(context.Background(), "bn-publisher", []string{"10.0.0.0/24"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "covers the existing member")
	require.Contains(t, err.Error(), "10.0.0.5/32")
	require.Equal(t, []string{"10.0.0.5/32"}, r.elements["bn-publisher"])
}

func TestAdd_RejectsCoveredCIDRInV6Set(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"2001:db8::/32"}))

	err := m.Add(context.Background(), "bn-publisher", []string{"2001:db8:1::/48"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "2001:db8::/32")
	require.Equal(t, []string{"2001:db8::/32"}, r.elements["bn-publisher6"])
}

func TestAdd_AllowsAdjacentAndExactDuplicateCIDRs(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	// Adjacency is not a conflict for nft, so we must not invent one.
	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"10.0.0.0/25", "10.0.0.128/25"}))
	// Re-adding an exact member is the silent no-op nft already makes it.
	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"10.0.0.0/25"}))
	require.Equal(t, []string{"10.0.0.0/25", "10.0.0.128/25", "10.0.0.0/25"}, r.elements["bn-publisher"])
}

func TestAdd_CrossFamilyPrefixesNeverCollide(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	require.NoError(t, m.Add(context.Background(), "bn-publisher",
		[]string{"10.0.0.0/24", "2001:db8::/32"}))
	require.Equal(t, []string{"10.0.0.0/24"}, r.elements["bn-publisher"])
	require.Equal(t, []string{"2001:db8::/32"}, r.elements["bn-publisher6"])
}

// --- Manager.Set ---

func TestSet_RejectsOverlappingList(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Set(context.Background(), "bn-publisher", []string{"10.7.0.0/24"}))

	err := m.Set(context.Background(), "bn-publisher", []string{"10.0.0.0/16", "10.0.5.0/24"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "10.0.0.0/16")
	require.Contains(t, err.Error(), "10.0.5.0/24")
	// A rejected full-replace must not have flushed the prior membership.
	require.Equal(t, []string{"10.7.0.0/24"}, r.elements["bn-publisher"],
		"a rejected set flushed the live membership")
}

// --- Manager.Create ---

func TestCreate_RejectsOverlappingCIDRs(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.0.0.0/24", "10.0.0.5/32"}, podCIDRArg("10.4.0.0/24"), false)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "10.0.0.0/24")
	require.Contains(t, err.Error(), "10.0.0.5/32")
	// Rejected before the lock and before any kernel or disk write, so neither
	// the registry nor the persisted .nft exists.
	require.Equal(t, 0, r.applyCount, "a rejected create applied to the kernel")
	require.NoFileExists(t, nftPath)
}

func TestCreate_ForceRejectsOverlappingCIDRs(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, []string{"10.7.0.0/24"}, "10.4.0.0/24")
	applyCountBefore := r.applyCount

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.0.0.0/16", "10.0.5.0/24"}, podCIDRArg("10.4.0.0/24"), true)
	require.Error(t, err)
	require.Equal(t, applyCountBefore, r.applyCount, "a rejected --force create re-applied the table")
	// The existing policy's membership survives untouched.
	require.Equal(t, []string{"10.7.0.0/24"}, r.elements["bn-publisher"])
}

func TestCreate_SnapshotRestoreRoundTripsMembershipExactly(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	// Membership that a fold would visibly change: two adjacent prefixes that
	// auto-merge would collapse into a single /24, plus a v6 member.
	authored := []string{"10.0.0.0/25", "10.0.0.128/25", "10.9.0.0/24"}
	require.NoError(t, m.Add(ctx, "bn-publisher", authored))
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"2001:db8::/32"}))

	// Creating a sibling re-renders the whole table (delete + add), so
	// bn-publisher's membership survives only via snapshot/restore.
	seedDenyPolicy(t, m, "bn-restricted", nil)

	require.Equal(t, authored, r.elements["bn-publisher"],
		"snapshot/restore narrowed or widened the authored membership (AC#2)")
	require.Equal(t, []string{"2001:db8::/32"}, r.elements["bn-publisher6"])
}

// --- daemon paths ---

func TestApplyMembership_PrunesCoveredCIDRsInsteadOfFailing(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedDenyPolicy(t, m, "bn-restricted", nil)

	// An upstream that supplies a host already inside a supplied prefix must not
	// wedge the reconcile: that would leave bn-restricted empty, a fail-open.
	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-restricted": {"10.99.0.0/16", "10.99.0.5/32", "10.98.0.0/16"},
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"10.99.0.0/16", "10.98.0.0/16"}, r.elements["bn-restricted"],
		"the covered /32 should be dropped and the rest left in input order")
}

func TestApplySets_PrunesCoveredCIDRs(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", nil, nil, "10.4.0.0/24")

	applied, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"2001:db8::/32", "2001:db8:1::/48", "10.0.0.0/24"}},
		nil)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"10.0.0.0/24"}, r.elements["bn-publisher"])
	require.Equal(t, []string{"2001:db8::/32"}, r.elements["bn-publisher6"],
		"the covered v6 /48 should be dropped")
}

func TestApplyMembership_LeavesNonOverlappingMembershipUntouched(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedDenyPolicy(t, m, "bn-restricted", nil)

	desired := []string{"10.99.0.0/16", "10.0.0.0/25", "10.0.0.128/25"}
	applied, err := m.ApplyMembership(context.Background(), map[string][]string{"bn-restricted": desired})
	require.NoError(t, err)
	require.True(t, applied)
	// Adjacency is not merged, so the daemon's diff sees the same element count
	// it supplied.
	require.Equal(t, desired, r.elements["bn-restricted"])
}

// --- compound (--reply-stamp) policies, AC#4 ---

func TestCompoundPolicy_OverlapChecksDoNotApply(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(ctx,
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		nil, podCIDRArg("10.4.0.0/24"), false)
	require.NoError(t, err)

	// These ip:port pairs would look like a containment conflict if the address
	// halves were compared as prefixes, but their sets are declared
	// `ipv4_addr . inet_service` with no flags interval, so nft stores them
	// side by side. Neither the reject nor the prune path may touch them.
	require.NoError(t, m.Add(ctx, "bn-backfill", []string{"10.0.0.0:443", "10.0.0.5:443"}))
	require.Equal(t, []string{"10.0.0.0 . 443", "10.0.0.5 . 443"}, r.elements["bn-backfill"])

	require.NoError(t, m.Set(ctx, "bn-backfill", []string{"10.0.0.0:443", "10.0.0.5:443"}))
	require.Equal(t, []string{"10.0.0.0 . 443", "10.0.0.5 . 443"}, r.elements["bn-backfill"])

	applied, err := m.ApplyMembership(ctx, map[string][]string{
		"bn-backfill": {"10.0.0.0:443", "10.0.0.5:443"},
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"10.0.0.0 . 443", "10.0.0.5 . 443"}, r.elements["bn-backfill"])
}

// --- Remove still works (AC#3) ---

func TestRemove_RemovesExplicitlyAddedCIDR(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"10.0.0.5/32", "10.9.0.0/24"}))

	// Because a covering member can never have been stored alongside it, the
	// exact authored spelling is always still there to delete (AC#3).
	require.NoError(t, m.Remove(ctx, "bn-publisher", []string{"10.0.0.5/32"}))
	require.Equal(t, []string{"10.9.0.0/24"}, r.elements["bn-publisher"])
}

// --- Manager.Remove: legible errors instead of raw nft text ---

func TestRemove_RejectsCIDRCoveredByAMember(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"10.0.0.0/24"}))

	// The policy DOES permit 10.0.0.5 (via the /24), so a bare "element does not
	// exist" would read like a bug. The error must name the member in the way.
	err := m.Remove(ctx, "bn-publisher", []string{"10.0.0.5/32"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "10.0.0.5/32")
	require.Contains(t, err.Error(), "covered by 10.0.0.0/24")
	require.Contains(t, err.Error(), "Removing part of a member is not supported")
	require.NotContains(t, err.Error(), "element does not exist", "leaked the raw nft error")
	require.Equal(t, []string{"10.0.0.0/24"}, r.elements["bn-publisher"])
}

func TestRemove_RejectsUnrelatedNonMember(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"10.0.0.0/24"}))

	err := m.Remove(ctx, "bn-publisher", []string{"192.168.99.0/24"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "192.168.99.0/24")
	require.Contains(t, err.Error(), "is not a member of policy")
	// Points at the command that lists what IS a member.
	require.Contains(t, err.Error(), "network policy show --name bn-publisher")
	require.NotContains(t, err.Error(), "element does not exist")
	require.Equal(t, []string{"10.0.0.0/24"}, r.elements["bn-publisher"])
}

// TestRemove_BatchWithOneAbsentEntryRemovesNothing pins the atomicity surprise:
// nft would fail the whole transaction and remove none of the valid entries,
// reporting a message that names neither the bad entry nor the policy. We now
// name the bad entry up front, and still remove nothing.
func TestRemove_BatchWithOneAbsentEntryRemovesNothing(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"10.0.0.0/24", "10.9.0.0/24"}))

	err := m.Remove(ctx, "bn-publisher", []string{"10.9.0.0/24", "192.168.99.0/24"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "192.168.99.0/24", "the error must name the offending entry")
	require.Equal(t, []string{"10.0.0.0/24", "10.9.0.0/24"}, r.elements["bn-publisher"],
		"a rejected batch must not remove the valid entries either")
}

// TestRemove_MatchesMemberStoredInNftsBareHostForm pins that the member-check is
// canonical rather than string-equal. A real kernel prints an interval set's /32
// as a bare host, so `network policy add --cidr 10.0.0.7/32` produces a live
// element spelled "10.0.0.7"; removing it by the /32 the operator originally
// typed has to match. Live membership is seeded directly here because the fake
// stores whatever it is handed rather than reproducing nft's print form.
func TestRemove_MatchesMemberStoredInNftsBareHostForm(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	r.elements["bn-publisher"] = []string{"10.0.0.7"}

	require.NoError(t, m.Remove(ctx, "bn-publisher", []string{"10.0.0.7/32"}))
	require.Empty(t, r.elements["bn-publisher"])
}

func TestRemove_V6MemberAndNonMember(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"2001:db8::/32"}))

	err := m.Remove(ctx, "bn-publisher", []string{"2001:db8:1::/48"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "covered by 2001:db8::/32")

	require.NoError(t, m.Remove(ctx, "bn-publisher", []string{"2001:db8::/32"}))
	require.Empty(t, r.elements["bn-publisher6"])
}

func TestRemove_CompoundPolicyMemberAndNonMember(t *testing.T) {
	ctx := context.Background()
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(ctx,
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		nil, podCIDRArg("10.4.0.0/24"), false)
	require.NoError(t, err)
	require.NoError(t, m.Add(ctx, "bn-backfill", []string{"10.0.0.5:443"}))

	// Compound elements have no containment relation, so a non-member gets the
	// plain not-a-member error, never the "covered by" one.
	err = m.Remove(ctx, "bn-backfill", []string{"10.0.0.9:443"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a member of policy")
	require.NotContains(t, err.Error(), "covered by")

	require.NoError(t, m.Remove(ctx, "bn-backfill", []string{"10.0.0.5:443"}))
	require.Empty(t, r.elements["bn-backfill"])
}
