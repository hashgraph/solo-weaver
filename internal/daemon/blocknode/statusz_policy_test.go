// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

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

	ce := CategoryEndpoints{
		CategoryPublisher:  {"10.1.0.1/32", "10.1.0.2/32"}, // one add
		CategoryPartner:    {"10.2.0.1/32"},                // all add (empty live)
		CategoryRestricted: {"10.3.0.0/24"},                // all add
		CategoryPeerBN:     {"10.30.5.7:43473"},            // compound add
	}

	deltas, err := computePolicyDeltas(context.Background(), l, ce)
	require.NoError(t, err)

	// Ordered by policy name; no-op deltas omitted (all four have changes here).
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-backfill", SetDelta: setDelta([]string{"10.30.5.7 . 43473"}, nil)},
		{Policy: "bn-partner", SetDelta: setDelta([]string{"10.2.0.1"}, nil)},
		{Policy: "bn-publisher", SetDelta: setDelta([]string{"10.1.0.2"}, nil)},
		{Policy: "bn-restricted", SetDelta: setDelta([]string{"10.3.0.0/24"}, nil)},
	}, deltas)
}

func TestComputePolicyDeltas_EmptyCategoryClearsSet(t *testing.T) {
	l := newFakeLister()
	l.elements["bn-publisher"] = []string{"10.1.0.1/32", "10.1.0.2/32"}

	// Present with an empty slice → clear the whole set.
	deltas, err := computePolicyDeltas(context.Background(), l, CategoryEndpoints{CategoryPublisher: {}})
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
	deltas, err := computePolicyDeltas(context.Background(), l, CategoryEndpoints{CategoryPartner: {"10.2.0.1/32"}})
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-partner", SetDelta: setDelta([]string{"10.2.0.1"}, nil)},
	}, deltas)
	require.NotContains(t, l.reads, "bn-publisher", "absent category's set must not be read")
}

func TestComputePolicyDeltas_NoOpDeltaOmitted(t *testing.T) {
	l := newFakeLister()
	// Live already matches desired (modulo spelling) → no delta.
	l.elements["bn-publisher"] = []string{"10.1.0.1", "10.1.0.2"}

	deltas, err := computePolicyDeltas(context.Background(), l,
		CategoryEndpoints{CategoryPublisher: {"10.1.0.2/32", "10.1.0.1/32"}})
	require.NoError(t, err)
	require.Empty(t, deltas)
}

func TestComputePolicyDeltas_UnmappedCategoryIgnored(t *testing.T) {
	l := newFakeLister()
	deltas, err := computePolicyDeltas(context.Background(), l, CategoryEndpoints{Category("mgmt"): {"10.9.0.1/32"}})
	require.NoError(t, err)
	require.Empty(t, deltas)
	require.Empty(t, l.reads, "unmapped category must not trigger any live-set read")
}

func TestComputePolicyDeltas_CompoundEndpointConversion(t *testing.T) {
	l := newFakeLister()
	l.elements["bn-backfill"] = []string{"10.30.5.7 . 43473"}

	deltas, err := computePolicyDeltas(context.Background(), l,
		CategoryEndpoints{CategoryPeerBN: {"10.30.5.8:43473"}})
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
		_, err := computePolicyDeltas(context.Background(), l, CategoryEndpoints{CategoryPeerBN: {bad}})
		require.Error(t, err, "expected %q to be rejected", bad)
		require.ErrorContains(t, err, "bn-backfill")
	}
}

func TestComputePolicyDeltas_LiveReadErrorPropagates(t *testing.T) {
	l := newFakeLister()
	l.err["bn-publisher"] = errors.New("nft boom")
	_, err := computePolicyDeltas(context.Background(), l, CategoryEndpoints{CategoryPublisher: {"10.1.0.1/32"}})
	require.Error(t, err)
	require.ErrorContains(t, err, "bn-publisher")
}

func TestReconcilePolicies_NilListerErrors(t *testing.T) {
	m := &TrafficShaperMonitor{} // no lister injected
	_, err := m.reconcilePolicies(context.Background(), CategoryEndpoints{CategoryPublisher: {"10.1.0.1/32"}})
	require.Error(t, err)
	require.ErrorContains(t, err, "live-set reader")
}

func TestReconcilePolicies_ComputesDeltas(t *testing.T) {
	l := newFakeLister()
	m := &TrafficShaperMonitor{lister: l}
	deltas, err := m.reconcilePolicies(context.Background(), CategoryEndpoints{CategoryPublisher: {"10.1.0.1/32"}})
	require.NoError(t, err)
	require.Equal(t, []PolicyDelta{
		{Policy: "bn-publisher", SetDelta: setDelta([]string{"10.1.0.1"}, nil)},
	}, deltas)
}

// setDelta builds a policy.SetDelta so tests express expectations as an
// (adds, deletes) pair.
func setDelta(adds, deletes []string) policy.SetDelta {
	return policy.SetDelta{Adds: adds, Deletes: deletes}
}
