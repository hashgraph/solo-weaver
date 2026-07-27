// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper

import (
	"context"
	"errors"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/stretchr/testify/require"
)

// fakeLister is a hand-written elementLister that returns seeded live membership
// per set and records which sets were read. A set with a configured err returns
// it instead of membership.
type fakeLister struct {
	elements map[string][]string
	err      map[string]error
	reads    []string
}

func newFakeLister() *fakeLister {
	return &fakeLister{elements: map[string][]string{}, err: map[string]error{}}
}

func (f *fakeLister) ListElements(_ context.Context, set string) ([]string, error) {
	f.reads = append(f.reads, set)
	if e, ok := f.err[set]; ok {
		return nil, e
	}
	return f.elements[set], nil
}

func TestComputePolicyDeltas_MapsCategoriesToPolicies(t *testing.T) {
	l := newFakeLister()
	l.elements["bn-publisher"] = []string{"10.1.0.1/32"}

	ce := categoryEndpoints{
		{Inbound, CategoryPublisher}:  {"10.1.0.1/32", "10.1.0.2/32"}, // one add
		{Inbound, CategoryPartner}:    {"10.2.0.1/32"},                // all add (empty live)
		{Inbound, CategoryRestricted}: {"10.3.0.0/24"},                // all add
		{Outbound, CategoryPartner}:   {"10.30.5.7:43473"},            // compound add
	}

	deltas, err := computePolicyDeltas(context.Background(), l, ce)
	require.NoError(t, err)

	// Ordered by policy name; no-op deltas omitted (all four have changes here).
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-backfill", SetDelta: setDelta([]string{"10.30.5.7 . 43473"}, nil)},
		{Policy: "bn-partner-out", SetDelta: setDelta([]string{"10.2.0.1"}, nil)},
		{Policy: "bn-publisher", SetDelta: setDelta([]string{"10.1.0.2"}, nil)},
		{Policy: "bn-restricted", SetDelta: setDelta([]string{"10.3.0.0/24"}, nil)},
	}, deltas)
}

// TestComputePolicyDeltas_PartnerSplitsByDirection proves the same partner
// category maps to two different sets depending on direction: inbound partner
// feeds bn-partner-out, outbound partner feeds the compound bn-backfill.
func TestComputePolicyDeltas_PartnerSplitsByDirection(t *testing.T) {
	l := newFakeLister()

	ce := categoryEndpoints{
		{Inbound, CategoryPartner}:  {"10.2.0.1/32"},
		{Outbound, CategoryPartner}: {"10.30.5.7:43473"},
	}

	deltas, err := computePolicyDeltas(context.Background(), l, ce)
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-backfill", SetDelta: setDelta([]string{"10.30.5.7 . 43473"}, nil)},
		{Policy: "bn-partner-out", SetDelta: setDelta([]string{"10.2.0.1"}, nil)},
	}, deltas)
}

func TestComputePolicyDeltas_EmptyCategoryClearsSet(t *testing.T) {
	l := newFakeLister()
	l.elements["bn-publisher"] = []string{"10.1.0.1/32", "10.1.0.2/32"}

	// Present with an empty slice → clear the whole set.
	deltas, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{Inbound, CategoryPublisher}: {}})
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-publisher", SetDelta: setDelta(nil, []string{"10.1.0.1", "10.1.0.2"})},
	}, deltas)
}

func TestComputePolicyDeltas_AbsentCategoryLeavesSetUntouched(t *testing.T) {
	l := newFakeLister()
	l.elements["bn-publisher"] = []string{"10.1.0.1/32"}

	// bn-publisher's category is absent entirely → no delta, and its live set is
	// never even read.
	deltas, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{Inbound, CategoryPartner}: {"10.2.0.1/32"}})
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-partner-out", SetDelta: setDelta([]string{"10.2.0.1"}, nil)},
	}, deltas)
	require.NotContains(t, l.reads, "bn-publisher", "absent category's set must not be read")
}

func TestComputePolicyDeltas_NoOpDeltaOmitted(t *testing.T) {
	l := newFakeLister()
	// Live already matches desired (modulo spelling) → no delta.
	l.elements["bn-publisher"] = []string{"10.1.0.1", "10.1.0.2"}

	deltas, err := computePolicyDeltas(context.Background(), l,
		categoryEndpoints{{Inbound, CategoryPublisher}: {"10.1.0.2/32", "10.1.0.1/32"}})
	require.NoError(t, err)
	require.Empty(t, deltas)
}

func TestComputePolicyDeltas_UnmappedCategoryIgnored(t *testing.T) {
	l := newFakeLister()
	deltas, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{Inbound, Category("mgmt")}: {"10.9.0.1/32"}})
	require.NoError(t, err)
	require.Empty(t, deltas)
	require.Empty(t, l.reads, "unmapped category must not trigger any live-set read")
}

// TestComputePolicyDeltas_PublicRecognizedButUnmapped confirms the public
// category produces no delta and no live-set read: it is deliberately not a
// monitor-owned set (bn-public-out stays port-match-driven), not an "unknown
// category" surprise.
func TestComputePolicyDeltas_PublicRecognizedButUnmapped(t *testing.T) {
	l := newFakeLister()
	for _, dir := range []Direction{Inbound, Outbound} {
		deltas, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{dir, CategoryPublic}: {"0.0.0.0/0"}})
		require.NoError(t, err)
		require.Empty(t, deltas)
	}
	require.Empty(t, l.reads, "public category must not trigger any live-set read")
}

func TestComputePolicyDeltas_CompoundEndpointConversion(t *testing.T) {
	l := newFakeLister()
	l.elements["bn-backfill"] = []string{"10.30.5.7 . 43473"}

	deltas, err := computePolicyDeltas(context.Background(), l,
		categoryEndpoints{{Outbound, CategoryPartner}: {"10.30.5.8:43473"}})
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-backfill", SetDelta: setDelta([]string{"10.30.5.8 . 43473"}, []string{"10.30.5.7 . 43473"})},
	}, deltas)
}

func TestComputePolicyDeltas_MalformedCompoundEndpointErrors(t *testing.T) {
	for _, bad := range []string{
		"10.30.5.8",       // missing port
		"10.30.5.8:-1",    // negative port
		"10.30.5.8:99999", // out-of-range port
		"::1:80",          // IPv6 host
	} {
		l := newFakeLister()
		_, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{Outbound, CategoryPartner}: {bad}})
		require.Error(t, err, "expected %q to be rejected", bad)
		require.ErrorContains(t, err, "bn-backfill")
	}
}

func TestComputePolicyDeltas_LiveReadErrorPropagates(t *testing.T) {
	l := newFakeLister()
	l.err["bn-publisher"] = errors.New("nft boom")
	_, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{Inbound, CategoryPublisher}: {"10.1.0.1/32"}})
	require.Error(t, err)
	require.ErrorContains(t, err, "bn-publisher")
}

func TestComputePolicyDeltas_ComputesDeltas(t *testing.T) {
	l := newFakeLister()
	deltas, err := computePolicyDeltas(context.Background(), l, categoryEndpoints{{Inbound, CategoryPublisher}: {"10.1.0.1/32"}})
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-publisher", SetDelta: setDelta([]string{"10.1.0.1"}, nil)},
	}, deltas)
}

func TestCanonicalDesiredMembership_MatchesDeltaRendering(t *testing.T) {
	ce := categoryEndpoints{
		{Inbound, CategoryPublisher}: {"10.1.0.2/32", "10.1.0.1/32"}, // /32 collapse + numeric order
		{Outbound, CategoryPartner}:  {"10.30.5.7:43473"},            // compound conversion
		{Inbound, Category("mgmt")}:  {"10.9.0.1"},                   // unmapped → dropped
	}

	m, err := canonicalDesiredMembership(ce)
	require.NoError(t, err)
	require.Equal(t, map[string][]string{
		"bn-publisher": {"10.1.0.1", "10.1.0.2"},
		"bn-backfill":  {"10.30.5.7 . 43473"},
	}, m)
}

func TestCanonicalDesiredMembership_MalformedCompoundErrors(t *testing.T) {
	_, err := canonicalDesiredMembership(categoryEndpoints{{Outbound, CategoryPartner}: {"not-an-ip-port"}})
	require.Error(t, err)
}

// setDelta builds a policy.SetDelta so tests express expectations as an
// (adds, deletes) pair.
func setDelta(adds, deletes []string) policy.SetDelta {
	return policy.SetDelta{Adds: adds, Deletes: deletes}
}

// TestCategoryBindings_PolicyNamesAreCanonical guards against categoryBindings
// drifting from the canonical BN policy set created by NetworkPolicyCreate
// (internal/workflows/steps/step_network_policy.go). A binding to a policy
// name that doesn't exist there makes ApplyMembership fail every time that
// category has any endpoint reported (Manager.ApplyMembership never calls
// create: "a name absent from the registry ... is an error") — this bit once,
// with CategoryPartner bound to "bn-partner" instead of "bn-partner-out".
// There is no shared export between the two packages, so this list must be
// kept in sync by hand; this test is what catches the next drift.
func TestCategoryBindings_PolicyNamesAreCanonical(t *testing.T) {
	canonicalBNPolicyNames := map[string]bool{
		"bn-publisher": true, "bn-subscriber-in": true, "bn-partner-out": true,
		"bn-public-out": true, "bn-status-in": true, "bn-status-out": true,
		"bn-mgmt-in": true, "bn-mgmt-out": true, "bn-restricted": true, "bn-backfill": true,
	}
	for key, b := range categoryBindings {
		if !canonicalBNPolicyNames[b.policyName] {
			t.Errorf("categoryBindings[%+v].policyName = %q is not one of the canonical "+
				"BN policy names created by NetworkPolicyCreate", key, b.policyName)
		}
	}
}
