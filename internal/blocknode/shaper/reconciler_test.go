// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFetcher returns seeded statusz payloads and records how many times each
// endpoint was read. A configured err is returned instead of the payload.
type fakeFetcher struct {
	inbound      NetworkData
	outbound     NetworkData
	inboundErr   error
	outboundErr  error
	inboundHits  int
	outboundHits int
}

func (f *fakeFetcher) InboundClients(context.Context) (NetworkData, error) {
	f.inboundHits++
	if f.inboundErr != nil {
		return NetworkData{}, f.inboundErr
	}
	return f.inbound, nil
}

func (f *fakeFetcher) OutboundClients(context.Context) (NetworkData, error) {
	f.outboundHits++
	if f.outboundErr != nil {
		return NetworkData{}, f.outboundErr
	}
	return f.outbound, nil
}

// fakeApplier records the membership it was asked to apply and returns a
// configurable (applied, err).
type fakeApplier struct {
	got     map[string][]string
	applied bool
	err     error
	calls   int
}

func (a *fakeApplier) ApplyMembership(_ context.Context, desired map[string][]string) (bool, error) {
	a.calls++
	a.got = desired
	if a.err != nil {
		return false, a.err
	}
	return a.applied, nil
}

// conn is a small helper to build an inbound/outbound NetworkConnection.
func conn(category, addr, port string) NetworkConnection {
	return NetworkConnection{Remote: Endpoint{Address: addr, Port: port}, Category: category}
}

func TestMembershipDigest_Deterministic(t *testing.T) {
	m := map[string][]string{
		"bn-publisher": {"10.1.0.1", "10.1.0.2"},
		"bn-partner":   {"10.2.0.1"},
	}
	require.Equal(t, membershipDigest(m), membershipDigest(m))
}

func TestMembershipDigest_OrderIndependent(t *testing.T) {
	a := map[string][]string{
		"bn-publisher": {"10.1.0.2", "10.1.0.1"},
		"bn-partner":   {"10.2.0.1"},
	}
	b := map[string][]string{
		"bn-partner":   {"10.2.0.1"},
		"bn-publisher": {"10.1.0.1", "10.1.0.2"},
	}
	require.Equal(t, membershipDigest(a), membershipDigest(b))
}

func TestMembershipDigest_DedupesMembership(t *testing.T) {
	withDup := map[string][]string{"bn-publisher": {"10.1.0.1", "10.1.0.1", "10.1.0.2"}}
	noDup := map[string][]string{"bn-publisher": {"10.1.0.2", "10.1.0.1"}}
	require.Equal(t, membershipDigest(noDup), membershipDigest(withDup))
}

func TestMembershipDigest_DistinguishesContent(t *testing.T) {
	a := map[string][]string{"bn-publisher": {"10.1.0.1"}}
	b := map[string][]string{"bn-publisher": {"10.1.0.2"}}
	require.NotEqual(t, membershipDigest(a), membershipDigest(b))

	// A present-but-empty set differs from an absent one.
	empty := map[string][]string{"bn-publisher": {}}
	absent := map[string][]string{}
	require.NotEqual(t, membershipDigest(empty), membershipDigest(absent))
}

func TestBucketizeEndpoints_CategorizesAndSeedsAllOwned(t *testing.T) {
	inbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "10.10.1.0/24", "*"),
		conn("partner", "10.20.1.0/24", "*"),
		conn("public", "0.0.0.0/0", "*"), // unmapped → skipped
	}}
	outbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("peer_bn", "10.30.5.7", "43473"),
	}}

	ce := bucketizeEndpoints(inbound, outbound)

	// All four owned categories present.
	require.Len(t, ce, 4)
	require.Contains(t, ce, CategoryPublisher)
	require.Contains(t, ce, CategoryPartner)
	require.Contains(t, ce, CategoryRestricted)
	require.Contains(t, ce, CategoryPeerBN)

	assert.Equal(t, []string{"10.10.1.0/24"}, ce[CategoryPublisher])
	assert.Equal(t, []string{"10.20.1.0/24"}, ce[CategoryPartner])
	// restricted reported nothing → present but empty (clears its set).
	assert.Empty(t, ce[CategoryRestricted])
	// peer_bn folds to a compound ip:port raw endpoint.
	assert.Equal(t, []string{"10.30.5.7:43473"}, ce[CategoryPeerBN])

	// The unmapped public category never appears.
	require.NotContains(t, ce, Category("public"))
}

func TestBucketizeEndpoints_SkipsWildcardAndEmptyPeerPort(t *testing.T) {
	outbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("peer_bn", "10.30.5.7", "43473"), // kept
		conn("peer_bn", "10.30.5.8", "*"),     // wildcard port → skipped
		conn("peer_bn", "10.30.5.9", ""),      // empty port → skipped
	}}

	ce := bucketizeEndpoints(NetworkData{}, outbound)
	assert.Equal(t, []string{"10.30.5.7:43473"}, ce[CategoryPeerBN])
}

func TestBucketizeEndpoints_EmptySnapshotSeedsAllOwnedEmpty(t *testing.T) {
	ce := bucketizeEndpoints(NetworkData{}, NetworkData{})
	require.Len(t, ce, 4)
	for c := range categoryBindings {
		assert.Empty(t, ce[c], "category %s should be present but empty", c)
	}
}

func TestDesiredMembership_MapsOwnedCategoriesToPolicies(t *testing.T) {
	ce := CategoryEndpoints{
		CategoryPublisher: {"10.1.0.1"},
		CategoryPeerBN:    {"10.30.5.7:43473"},
		Category("mgmt"):  {"10.9.0.1"}, // unmapped → dropped
	}
	m := desiredMembership(ce)
	require.Equal(t, map[string][]string{
		"bn-publisher": {"10.1.0.1"},
		"bn-backfill":  {"10.30.5.7:43473"},
	}, m)
}

func TestReconciler_Check_DigestsDesired(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "10.10.1.0/24", "*"),
		}},
		outbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("peer_bn", "10.30.5.7", "43473"),
		}},
	}
	r := &Reconciler{fetcher: f}

	result, err := r.Check(context.Background())
	require.NoError(t, err)

	// The digest is exactly the digest of the canonical desired membership
	// derived from the same snapshot — no nft read/write happened (nil
	// lister/applier untouched).
	ce := bucketizeEndpoints(f.inbound, f.outbound)
	wantCanon, err := canonicalDesiredMembership(ce)
	require.NoError(t, err)
	require.Equal(t, membershipDigest(wantCanon), result.Digest)
	require.Equal(t, wantCanon, result.Desired)
}

func TestReconciler_Check_DigestIgnoresSpellingEquivalence(t *testing.T) {
	// A /32-suffixed host and its bare-address spelling are canonically
	// identical, and so are the two "compound" spellings statusz vs. the nft
	// rendering would use — the digest must not distinguish them, since the
	// actual applied membership would be identical either way.
	bare := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "10.1.0.1", "*"),
	}}}
	slash32 := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "10.1.0.1/32", "*"),
	}}}

	rBare := &Reconciler{fetcher: bare}
	rSlash32 := &Reconciler{fetcher: slash32}

	resBare, err := rBare.Check(context.Background())
	require.NoError(t, err)
	resSlash32, err := rSlash32.Check(context.Background())
	require.NoError(t, err)

	require.Equal(t, resBare.Digest, resSlash32.Digest)
}

func TestReconciler_Check_PropagatesFetchError(t *testing.T) {
	r := &Reconciler{fetcher: &fakeFetcher{inboundErr: errors.New("statusz down")}}
	_, err := r.Check(context.Background())
	require.Error(t, err)
}

func TestReconciler_Apply_AppliesOnlyChangedPolicies(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "10.1.0.1/32", "*"), // will change
			conn("partner", "10.2.0.1/32", "*"),   // already live → unchanged
		}},
		outbound: NetworkData{},
	}
	lister := newFakeLister()
	// bn-partner-out already matches desired; bn-publisher differs; bn-restricted
	// and bn-backfill are seeded empty and live-empty → no change.
	lister.elements["bn-partner-out"] = []string{"10.2.0.1"}
	lister.elements["bn-publisher"] = []string{"10.9.9.9"}

	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	// Only bn-publisher changed; it is the only policy handed to the applier.
	require.Equal(t, 1, applier.calls)
	require.Equal(t, map[string][]string{"bn-publisher": {"10.1.0.1/32"}}, applier.got)

	assert.Equal(t, []string{"bn-publisher"}, res.Applied)
	// The other three owned policies are reported unchanged.
	assert.Equal(t, []string{"bn-backfill", "bn-partner-out", "bn-restricted"}, res.Unchanged)
	assert.NotEmpty(t, res.Digest)
}

func TestReconciler_Apply_NoChangesTakesNoApply(t *testing.T) {
	f := &fakeFetcher{} // empty snapshot → all owned sets desired-empty
	lister := newFakeLister()
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, applier.calls, "no deltas → applier must not be called")
	assert.Empty(t, res.Applied)
	assert.Equal(t, ownedPolicyNames(), res.Unchanged)
}

func TestReconciler_Apply_LockHeldReportsNothingApplied(t *testing.T) {
	f := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "10.1.0.1/32", "*"),
	}}}
	lister := newFakeLister()
	lister.elements["bn-publisher"] = []string{"10.9.9.9"}
	applier := &fakeApplier{applied: false} // operator lock held → skipped
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, applier.calls)
	assert.Empty(t, res.Applied, "lock held → nothing reported applied")
	// The changed policy must not silently vanish from the result: it is
	// reported skipped (still out of sync), not folded into unchanged.
	assert.Equal(t, []string{"bn-publisher"}, res.Skipped)
	assert.Equal(t, []string{"bn-backfill", "bn-partner-out", "bn-restricted"}, res.Unchanged)
}

func TestReconciler_Apply_PropagatesApplyError(t *testing.T) {
	f := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "10.1.0.1/32", "*"),
	}}}
	lister := newFakeLister()
	lister.elements["bn-publisher"] = []string{"10.9.9.9"}
	applier := &fakeApplier{err: errors.New("nft boom")}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	_, err := r.Apply(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "nft boom")
}
