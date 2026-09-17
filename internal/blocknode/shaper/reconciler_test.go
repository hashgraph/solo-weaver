// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
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

// fakeApplier records the membership and listener ports it was asked to apply in
// a single ApplySets call and returns a configurable (applied, err).
type fakeApplier struct {
	got      map[string][]string // last ApplySets membership input
	gotPorts map[string][]string // last ApplySets ports input
	applied  bool                // returned by ApplySets
	err      error               // returned by ApplySets
	calls    int                 // ApplySets calls
}

func (a *fakeApplier) ApplySets(_ context.Context, membership, ports map[string][]string) (bool, error) {
	a.calls++
	a.got = membership
	a.gotPorts = ports
	if a.err != nil {
		return false, a.err
	}
	return a.applied, nil
}

// conn is a small helper to build an inbound/outbound NetworkConnection with only
// a remote (its local port is unset, so it drives membership but not listener
// ports).
func conn(category, addr, port string) NetworkConnection {
	return NetworkConnection{Remote: Endpoint{Address: addr, Port: port}, Category: category}
}

// inconn builds an inbound NetworkConnection carrying a local listener port (the
// BN side), used to exercise listener-port derivation.
func inconn(category, localPort string) NetworkConnection {
	return NetworkConnection{
		Local:    Endpoint{Address: "192.168.1.119", Port: localPort},
		Remote:   Endpoint{Address: "*", Port: "*"},
		Category: category,
	}
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
		conn("public", "0.0.0.0/0", "*"), // recognized but unmapped → skipped
	}}
	outbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("partner", "10.30.5.7", "43473"), // outbound partner → bn-backfill
	}}

	ce := bucketizeEndpoints(inbound, outbound, true, true)

	// All four owned bindings present.
	require.Len(t, ce, 4)
	require.Contains(t, ce, bindingKey{Inbound, CategoryPublisher})
	require.Contains(t, ce, bindingKey{Inbound, CategoryPartner})
	require.Contains(t, ce, bindingKey{Inbound, CategoryRestricted})
	require.Contains(t, ce, bindingKey{Outbound, CategoryPartner})

	assert.Equal(t, []string{"10.10.1.0/24"}, ce[bindingKey{Inbound, CategoryPublisher}])
	assert.Equal(t, []string{"10.20.1.0/24"}, ce[bindingKey{Inbound, CategoryPartner}])
	// restricted reported nothing → present but empty (clears its set).
	assert.Empty(t, ce[bindingKey{Inbound, CategoryRestricted}])
	// outbound partner folds to a compound ip:port raw endpoint.
	assert.Equal(t, []string{"10.30.5.7:43473"}, ce[bindingKey{Outbound, CategoryPartner}])

	// The unmapped public category never appears, on either direction.
	require.NotContains(t, ce, bindingKey{Inbound, CategoryPublic})
	require.NotContains(t, ce, bindingKey{Outbound, CategoryPublic})
}

func TestBucketizeEndpoints_SkipsWildcardAndEmptyBackfillPort(t *testing.T) {
	outbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("partner", "10.30.5.7", "43473"), // kept
		conn("partner", "10.30.5.8", "*"),     // wildcard port → skipped
		conn("partner", "10.30.5.9", ""),      // empty port → skipped
	}}

	ce := bucketizeEndpoints(NetworkData{}, outbound, false, true)
	assert.Equal(t, []string{"10.30.5.7:43473"}, ce[bindingKey{Outbound, CategoryPartner}])
}

// TestBucketizeEndpoints_NormalizesBareHostToSlash32 pins the fix for the
// statusz-derived membership apply: a connected peer is reported by statusz as a
// bare host IP, but the policy layer validates every plain-set element as a CIDR
// and rejects a bare address. bucketize must therefore normalize a bare IPv4
// host to an explicit /32 while leaving an already-masked CIDR untouched, so the
// desired membership fed to ApplySets is always valid.
func TestBucketizeEndpoints_NormalizesBareHostToSlash32(t *testing.T) {
	inbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "198.51.100.234", "*"), // bare host → /32
		conn("partner", "198.51.100.0/24", "*"),  // already a CIDR → unchanged
	}}

	ce := bucketizeEndpoints(inbound, NetworkData{}, true, false)

	assert.Equal(t, []string{"198.51.100.234/32"}, ce[bindingKey{Inbound, CategoryPublisher}])
	assert.Equal(t, []string{"198.51.100.0/24"}, ce[bindingKey{Inbound, CategoryPartner}])
}

// TestBucketizeEndpoints_IPv6 mirrors the /32 normalization for IPv6: a bare v6
// peer becomes an explicit /128 (the form the policy layer accepts), and a
// compound (outbound partner) v6 endpoint is bracketed so its ip:port token
// parses unambiguously downstream in CompoundElement.
func TestBucketizeEndpoints_IPv6(t *testing.T) {
	inbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("publisher", "2001:db8::1", "*"),   // bare v6 host → /128
		conn("partner", "2001:db8:a::/48", "*"), // already a CIDR → unchanged
	}}
	outbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("partner", "2001:db8::2", "43473"), // compound v6 → bracketed ip:port
	}}

	ce := bucketizeEndpoints(inbound, outbound, true, true)

	assert.Equal(t, []string{"2001:db8::1/128"}, ce[bindingKey{Inbound, CategoryPublisher}])
	assert.Equal(t, []string{"2001:db8:a::/48"}, ce[bindingKey{Inbound, CategoryPartner}])
	assert.Equal(t, []string{"[2001:db8::2]:43473"}, ce[bindingKey{Outbound, CategoryPartner}])
}

// TestReconciler_Apply_NormalizesBareHostMembership is the end-to-end guard: a
// statusz snapshot carrying a bare host IP must reach ApplySets as an explicit
// /32 CIDR (the form policy.Manager.ApplySets accepts), not the bare address
// that surfaced the original apply failure.
func TestReconciler_Apply_NormalizesBareHostMembership(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "198.51.100.234", "*"),
		}},
		outbound: NetworkData{},
	}
	lister := newFakeLister()
	lister.elements["bn-publisher"] = []string{"10.9.9.9"} // differs → change

	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	require.Equal(t, map[string][]string{"bn-publisher": {"198.51.100.234/32"}}, applier.got)
	assert.Equal(t, []string{"bn-publisher"}, res.Applied)
}

// A reported direction seeds every binding it feeds, even categories the payload
// never mentions — that is what clears those sets.
func TestBucketizeEndpoints_ReportedDirectionSeedsItsOwnedBindingsEmpty(t *testing.T) {
	// A payload carrying only the unmapped public category: it reported
	// something, but nothing that lands in an owned binding.
	inbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("public", "0.0.0.0/0", "*"),
	}}

	ce := bucketizeEndpoints(inbound, NetworkData{}, true, false)

	require.Len(t, ce, 3, "all three inbound bindings, and only those")
	for k := range categoryBindings {
		if k.dir != Inbound {
			continue
		}
		require.Contains(t, ce, k)
		assert.Empty(t, ce[k], "binding %+v should be present but empty", k)
	}
}

// An unreported direction seeds nothing, so its sets are left alone.
func TestBucketizeEndpoints_UnreportedDirectionIsAbsent(t *testing.T) {
	outbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		conn("partner", "10.30.5.7", "43473"),
	}}

	// Inbound reported nothing; outbound reported a peer.
	ce := bucketizeEndpoints(NetworkData{}, outbound, false, true)
	require.Len(t, ce, 1)
	require.Contains(t, ce, bindingKey{Outbound, CategoryPartner})

	// Neither reported: nothing is desired, so nothing is touched.
	require.Empty(t, bucketizeEndpoints(NetworkData{}, NetworkData{}, false, false))
}

func TestDesiredMembership_MapsOwnedCategoriesToPolicies(t *testing.T) {
	ce := categoryEndpoints{
		{Inbound, CategoryPublisher}: {"10.1.0.1"},
		{Outbound, CategoryPartner}:  {"10.30.5.7:43473"},
		{Inbound, Category("mgmt")}:  {"10.9.0.1"}, // unmapped → dropped
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
			conn("partner", "10.30.5.7", "43473"),
		}},
	}
	r := &Reconciler{fetcher: f}

	result, err := r.Check(context.Background())
	require.NoError(t, err)

	// The digest is exactly the digest of the canonical desired membership AND
	// listener ports derived from the same snapshot — no nft read/write happened
	// (nil lister/applier untouched).
	ce := bucketizeEndpoints(f.inbound, f.outbound, reported(f.inbound), reported(f.outbound))
	wantCanon, err := canonicalDesiredMembership(ce)
	require.NoError(t, err)
	wantPorts := desiredPorts(f.inbound)
	require.Equal(t, membershipDigest(combinedCanonical(wantCanon, wantPorts)), result.Digest)
	require.Equal(t, wantCanon, result.Desired)
	require.Equal(t, wantPorts, result.DesiredPorts)
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
	// is seeded empty and live-empty → no change. bn-backfill is never diffed: its
	// outbound payload reported nothing, so it is withheld.
	lister.elements["bn-partner-out"] = []string{"10.2.0.1"}
	lister.elements["bn-publisher"] = []string{"10.9.9.9"}

	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	// Only bn-publisher changed; it is the only policy in the membership batch. No
	// inbound local.port was reported, so every managed-ports set is desired-empty
	// and live-empty → the ports batch is empty (one atomic ApplySets call).
	require.Equal(t, 1, applier.calls)
	require.Equal(t, map[string][]string{"bn-publisher": {"10.1.0.1/32"}}, applier.got)
	require.Empty(t, applier.gotPorts, "no port change → empty ports batch")

	assert.Equal(t, []string{"bn-publisher"}, res.Applied)
	assert.Equal(t, exclude(ownedSetNames(), "bn-publisher"), res.Unchanged)
	// bn-publisher gained a member rather than losing its last one.
	assert.Empty(t, res.Cleared)
	assert.NotEmpty(t, res.Digest)
}

func TestReconciler_Apply_NoChangesStillCallsApplierWithEmptyBatches(t *testing.T) {
	f := &fakeFetcher{} // both statusz calls reported nothing → everything withheld
	lister := newFakeLister()
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	// The applier is still invoked, with both batches empty: writing the kernel
	// is only half its job, and the other half — re-persisting the on-disk
	// artifact — has to run on a converged node too. A converged node produces no
	// deltas indefinitely, so skipping the call here would leave its artifact
	// permanently stale while every tick reported success.
	require.Equal(t, 1, applier.calls, "a no-delta tick must still reach the applier so persistence runs")
	require.Empty(t, applier.got, "no membership change → empty membership batch")
	require.Empty(t, applier.gotPorts, "no port change → empty ports batch")

	// Neither statusz call reported an endpoint, so no live set is diffed.
	assert.Empty(t, lister.reads, "an unreported call diffs no live set")
	assert.Empty(t, res.Applied)
	assert.Equal(t, ownedSetNames(), res.Unchanged)
	assert.Empty(t, res.Cleared)
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
	assert.Equal(t, exclude(ownedSetNames(), "bn-publisher"), res.Unchanged)
}

// quarantined is a fake lister with a live member in every owned membership set;
// bn-restricted's is a quarantined peer.
func quarantined() *fakeLister {
	l := newFakeLister()
	l.elements["bn-publisher"] = []string{"10.1.0.1"}
	l.elements["bn-partner-out"] = []string{"10.2.0.1"}
	l.elements["bn-restricted"] = []string{"10.3.0.1"}
	l.elements["bn-backfill"] = []string{"10.30.5.7 . 43473"}
	return l
}

// An empty /statusz/inbound leaves all seven sets it feeds exactly as they are.
func TestReconciler_Apply_EmptyInboundLeavesInboundFedSetsUntouched(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{}, // 200, activeEndpoints: []
		outbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("partner", "10.30.5.7", "43473"),
		}},
	}
	lister := quarantined()
	// Live listener ports too: all four managed `_ports` sets ride on the inbound
	// call, so they are withheld with it.
	for _, p := range managedPortsPolicyNames() {
		lister.elements[policy.PortsSetName(p)] = []string{"40984"}
	}
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	// Nothing inbound-fed reaches the applier, so neither the kernel nor the
	// persisted artifact loses a member: policy.Manager re-renders the .nft from a
	// live snapshot, so a set it never writes keeps what it had.
	require.Equal(t, 1, applier.calls)
	assert.Empty(t, applier.got, "bn-backfill already matches live → no membership write")
	assert.Empty(t, applier.gotPorts)

	// Not diffed, not just written back with the old value.
	for _, set := range []string{
		"bn-partner-out", "bn-partner-out_ports", "bn-public-out_ports",
		"bn-publisher", "bn-publisher_ports", "bn-restricted", "bn-subscriber-in_ports",
	} {
		assert.NotContains(t, lister.reads, set, "%s must not be diffed when its statusz call reported nothing", set)
	}

	assert.Equal(t, ownedSetNames(), res.Unchanged, "withheld sets are reported unchanged")
	assert.Empty(t, res.Cleared, "withholding can never lift a quarantine")
}

// The other half of the empty-response guarantee: withholding the sets is no use
// if the same tick discards the cached address the next tick's fallback is built
// from, because the tick after that clears the set instead.
func TestReconciler_Apply_EmptyInboundKeepsTheDNSFallbackForTheNextTick(t *testing.T) {
	cachePath := dnsCachePathFor(filepath.Join(t.TempDir(), "policy.nft"))
	roster := nd(conn("publisher", "peer.example.com", "*"))
	resolver := &fakeResolver{answers: map[string][]string{"peer.example.com": {"10.1.0.1"}}}
	f := &fakeFetcher{inbound: roster}
	lister := newFakeLister()
	applier := &fakeApplier{applied: true}
	r := &Reconciler{
		fetcher: f, resolver: resolver, lister: lister, applier: applier,
		dnsCachePath: cachePath,
	}

	// Tick 1: the peer resolves, lands in bn-publisher, and its address is
	// persisted as the last-known-good fallback.
	res, err := r.Apply(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"bn-publisher": {"10.1.0.1/32"}}, applier.got)
	require.Equal(t, []string{"bn-publisher"}, res.Applied)
	require.Contains(t, loadDNSCache(cachePath), "peer.example.com")
	persisted, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	// The kernel now holds what tick 1 wrote. Then the resolver goes down and
	// statusz answers 200 with activeEndpoints: [].
	lister.elements["bn-publisher"] = []string{"10.1.0.1"}
	resolver.transient = map[string]bool{"peer.example.com": true}
	f.inbound = NetworkData{}

	// Tick 2: the sets are withheld, and the fallback is left exactly as it was.
	res, err = r.Apply(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Cleared)
	after, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	require.Equal(t, persisted, after,
		"an empty statusz response must not disturb the fallback the next tick reads")

	// Tick 3: statusz reports the peer again, the resolver is still down.
	f.inbound = roster

	res, err = r.Apply(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.Cleared, "an empty response must not clear a set two ticks later either")
	assert.Empty(t, applier.got, "the stale address still matches live, so nothing is rewritten")
	assert.Equal(t,
		[]NamedIssue{{Name: "peer.example.com", Policies: []string{"bn-publisher"}}},
		res.Stale, "the peer is served stale, and the operator is told so")
}

// bn-backfill is the only set /statusz/outbound feeds, so it is withheld alone.
func TestReconciler_Apply_EmptyOutboundLeavesBackfillUntouched(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "10.1.0.1", "*"),
			conn("partner", "10.2.0.1", "*"),
			conn("restricted", "10.3.0.1", "*"),
		}},
		outbound: NetworkData{}, // 200, activeEndpoints: []
	}
	lister := quarantined()
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	assert.NotContains(t, lister.reads, "bn-backfill")
	assert.NotContains(t, applier.got, "bn-backfill")
	assert.Empty(t, res.Cleared)
}

// A populated payload that omits a category still clears it, on that tick.
func TestReconciler_Apply_PopulatedPayloadStillClearsAMissingCategory(t *testing.T) {
	f := &fakeFetcher{
		// The block node reports its publisher and partner peers but no restricted
		// peer: the quarantine has genuinely been lifted.
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "10.1.0.1", "*"),
			conn("partner", "10.2.0.1", "*"),
		}},
		outbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("partner", "10.30.5.7", "43473"),
		}},
	}
	lister := quarantined()
	// A live listener port too: conn() carries no local.port, so the reported
	// inbound call desires bn-publisher_ports empty and clears it.
	lister.elements[policy.PortsSetName("bn-publisher")] = []string{"40984"}
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	// bn-restricted is present-and-empty in the batch: that is what clears it.
	require.Contains(t, applier.got, "bn-restricted")
	assert.Empty(t, applier.got["bn-restricted"])
	require.Contains(t, applier.gotPorts, "bn-publisher")
	assert.Empty(t, applier.gotPorts["bn-publisher"])
	// A ports set is reported by its nft set name, not its policy name.
	assert.Equal(t, []string{"bn-publisher_ports", "bn-restricted"}, res.Cleared)
}

// A populated payload that names every owned category, matching live, changes nothing.
func TestReconciler_Apply_PopulatedPayloadMatchingLiveChangesNothing(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "10.1.0.1", "*"),
			conn("partner", "10.2.0.1", "*"),
			conn("restricted", "10.3.0.1", "*"),
		}},
		outbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("partner", "10.30.5.7", "43473"),
		}},
	}
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: quarantined(), applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, applier.calls)
	assert.Empty(t, applier.got)
	assert.Empty(t, applier.gotPorts)
	assert.Empty(t, res.Applied)
	assert.Equal(t, ownedSetNames(), res.Unchanged)
	assert.Empty(t, res.Cleared)
}

// Lock held: nothing reached the kernel, so the pending clear is Skipped, not Cleared.
func TestReconciler_Apply_ClearedOnlyReportedWhenActuallyWritten(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("publisher", "10.1.0.1", "*"),
			conn("partner", "10.2.0.1", "*"),
		}},
		outbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("partner", "10.30.5.7", "43473"),
		}},
	}
	applier := &fakeApplier{applied: false} // operator lock held
	r := &Reconciler{fetcher: f, lister: quarantined(), applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	assert.Empty(t, res.Cleared, "nothing was written, so nothing was cleared")
	assert.Equal(t, []string{"bn-restricted"}, res.Skipped, "the pending clear is reported as skipped instead")
}

// An unreported call leaves its sets out of the desired maps entirely, so the
// digest cannot change on their account.
func TestReconciler_Check_UnreportedCallLeavesItsSetsOutOfDesired(t *testing.T) {
	f := &fakeFetcher{
		inbound: NetworkData{}, // reported nothing
		outbound: NetworkData{ActiveEndpoints: []NetworkConnection{
			conn("partner", "10.30.5.7", "43473"),
		}},
	}
	r := &Reconciler{fetcher: f}

	res, err := r.Check(context.Background())
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{"bn-backfill": {"10.30.5.7 . 43473"}}, res.Desired)
	assert.Empty(t, res.DesiredPorts, "every managed-ports set rides on the inbound call")
	assert.NotEmpty(t, res.Digest, "a digest is still produced, so the daemon's gate keeps working")
}

// nil, not a map of empty slices, so the ports sets are left alone.
func TestDesiredPorts_UnreportedInboundWithholdsEveryManagedSet(t *testing.T) {
	assert.Nil(t, desiredPorts(NetworkData{}))
}

// exclude returns all minus the dropped names, preserving order — a small helper
// so unchanged-set expectations track ownedSetNames() rather than hardcoding the
// full list.
func exclude(all []string, drop ...string) []string {
	dropSet := make(map[string]struct{}, len(drop))
	for _, d := range drop {
		dropSet[d] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, s := range all {
		if _, ok := dropSet[s]; !ok {
			out = append(out, s)
		}
	}
	return out
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

func TestDesiredPorts_DerivesPerFacilityFromInboundLocalPort(t *testing.T) {
	inbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("publisher", "40984"),
		inconn("partner", "40980"),
		inconn("public", "40980"),     // subscriber
		inconn("public", "40981"),     // block-access
		inconn("public", "40982"),     // server-status — public to everyone
		inconn("restricted", "49999"), // no port binding → ignored
	}}

	got := desiredPorts(inbound)

	// publisher and partner map to a single set each; the public union
	// (including server-status 40982) feeds both public-facing sets.
	assert.Equal(t, map[string][]string{
		"bn-publisher":     {"40984"},
		"bn-partner-out":   {"40980"},
		"bn-subscriber-in": {"40980", "40981", "40982"},
		"bn-public-out":    {"40980", "40981", "40982"},
	}, got)
}

func TestDesiredPorts_SeedsAllManagedPresentAndSkipsWildcardEmpty(t *testing.T) {
	inbound := NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("publisher", "*"), // wildcard local port → skipped
		inconn("partner", ""),    // empty local port → skipped
	}}

	got := desiredPorts(inbound)

	// Every managed-ports policy is present but empty (present-vs-absent is load
	// bearing: present-empty clears the set, matching the membership contract).
	require.Len(t, got, 4)
	for _, name := range []string{"bn-publisher", "bn-partner-out", "bn-subscriber-in", "bn-public-out"} {
		require.Contains(t, got, name)
		assert.Empty(t, got[name], name)
	}
}

func TestReconciler_Apply_ReconcilesListenerPorts(t *testing.T) {
	// The public category has no membership binding, so a public inbound endpoint
	// drives ONLY the listener-port pass — an isolated ports-only reconcile.
	f := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("public", "40982"),
	}}}
	lister := newFakeLister() // all sets live-empty
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, applier.calls)
	require.Empty(t, applier.got, "public has no membership binding → empty membership batch")
	require.Equal(t, map[string][]string{
		"bn-subscriber-in": {"40982"},
		"bn-public-out":    {"40982"},
	}, applier.gotPorts)

	// The changed ports are reported by their `<name>_ports` nft set names.
	assert.Equal(t, []string{"bn-public-out_ports", "bn-subscriber-in_ports"}, res.Applied)
	assert.NotContains(t, res.Unchanged, "bn-public-out_ports")
}

func TestReconciler_Apply_PortLockHeldReportsPortsSkipped(t *testing.T) {
	f := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("public", "40982"),
	}}}
	lister := newFakeLister()
	applier := &fakeApplier{applied: false} // operator lock held → skipped
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, applier.calls)
	assert.Empty(t, res.Applied)
	assert.Equal(t, []string{"bn-public-out_ports", "bn-subscriber-in_ports"}, res.Skipped)
}

func TestReconciler_Apply_PropagatesPortApplyError(t *testing.T) {
	f := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("public", "40982"),
	}}}
	lister := newFakeLister()
	applier := &fakeApplier{err: errors.New("nft ports boom")}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	_, err := r.Apply(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "nft ports boom")
}

func TestReconciler_Check_DigestChangesOnPortsOnly(t *testing.T) {
	// Two snapshots with identical membership (public has no membership binding)
	// but different derived listener ports — the digest must still differ so the
	// daemon's digest-based change detection never misses a ports-only update.
	base := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("public", "40982"),
	}}}
	changed := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		inconn("public", "40999"),
	}}}
	resBase, err := (&Reconciler{fetcher: base}).Check(context.Background())
	require.NoError(t, err)
	resChanged, err := (&Reconciler{fetcher: changed}).Check(context.Background())
	require.NoError(t, err)

	require.Equal(t, resBase.Desired, resChanged.Desired, "membership is identical")
	require.NotEqual(t, resBase.Digest, resChanged.Digest, "ports-only change must move the digest")
}

func TestReconciler_Apply_MembershipAndPortsAppliedInOneAtomicCall(t *testing.T) {
	// A publisher inbound endpoint carries both a remote (membership) and a
	// local.port (listener port), so both dimensions change this tick — and must
	// be handed to the applier in a SINGLE ApplySets call, so an operator cannot
	// grab the lock between a membership write and a ports write and leave one
	// dimension stale.
	f := &fakeFetcher{inbound: NetworkData{ActiveEndpoints: []NetworkConnection{
		{
			Local:    Endpoint{Address: "192.0.2.10", Port: "40984"},
			Remote:   Endpoint{Address: "198.51.100.1", Port: "*"},
			Category: "publisher",
		},
	}}}
	lister := newFakeLister()
	applier := &fakeApplier{applied: true}
	r := &Reconciler{fetcher: f, lister: lister, applier: applier}

	res, err := r.Apply(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, applier.calls, "both dimensions go through one atomic ApplySets call")
	require.Equal(t, map[string][]string{"bn-publisher": {"198.51.100.1/32"}}, applier.got)
	require.Equal(t, map[string][]string{"bn-publisher": {"40984"}}, applier.gotPorts)
	assert.Equal(t, []string{"bn-publisher", "bn-publisher_ports"}, res.Applied)
	assert.Empty(t, res.Skipped)
	assert.Equal(t, exclude(ownedSetNames(), "bn-publisher", "bn-publisher_ports"), res.Unchanged)
}
