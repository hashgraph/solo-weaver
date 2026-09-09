// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers from a fixed table. A name mapped to a nil slice is
// treated as unresolvable, so a test can exercise the failure path without a
// resolver outage.
//
// The mutex is required, not defensive: resolveHosts looks every name up at
// once, so recording a call is a concurrent write. The Resolver contract calls
// for concurrency safety and a fake that ignores it fails under -race.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]string
	// answersV6 seeds AAAA-only names: present here (with a non-empty slice)
	// and absent from answers means "resolves, but only to AAAA".
	answersV6 map[string][]string
	calls     []string
	// hang names block until their own context is cancelled, standing in for a
	// resolver that never answers (a dropped query under the default
	// resolv.conf spends longer than resolveTimeout on one name).
	hang map[string]bool
}

func (f *fakeResolver) LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	f.calls = append(f.calls, host)
	hang := f.hang[host]
	f.mu.Unlock()

	if hang {
		<-ctx.Done()
		return nil, errorx.ExternalError.New("timed out: %s", host)
	}

	raw, ok := f.answers[host]
	if !ok || len(raw) == 0 {
		return nil, errorx.ExternalError.New("no such host: %s", host)
	}
	addrs := make([]netip.Addr, 0, len(raw))
	for _, s := range raw {
		addrs = append(addrs, netip.MustParseAddr(s))
	}
	return addrs, nil
}

func (f *fakeResolver) LookupIPv6(_ context.Context, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	raw, ok := f.answersV6[host]
	f.mu.Unlock()
	if !ok || len(raw) == 0 {
		return nil, errorx.ExternalError.New("no such host: %s", host)
	}
	addrs := make([]netip.Addr, 0, len(raw))
	for _, s := range raw {
		addrs = append(addrs, netip.MustParseAddr(s))
	}
	return addrs, nil
}

func nd(conns ...NetworkConnection) NetworkData {
	return NetworkData{ActiveEndpoints: conns}
}

func TestIsRemoteFQDN(t *testing.T) {
	tests := map[string]bool{
		"peer.example.com": true,
		"a.b.c.example":    true,
		"host-1.example":   true,

		// statusz vocabulary, not names
		"":  false,
		"*": false,

		// literals
		"10.1.0.1":      false,
		"10.1.0.0/24":   false,
		"2001:db8::1":   false,
		"2001:db8::/32": false,

		// single label: resolving it would depend on the host's search domain
		"blocknode": false,

		// not a hostname
		"peer.example.com:443": false,
		"peer_1.example.com":   false,
	}
	for in, want := range tests {
		require.Equalf(t, want, isRemoteFQDN(in), "isRemoteFQDN(%q)", in)
	}
}

func TestRemoteFQDNs_DistinctAcrossPayloadsInFirstSeenOrder(t *testing.T) {
	inbound := nd(
		conn("publisher", "b.example.com", "*"),
		conn("partner", "10.1.0.1", "*"),
		conn("restricted", "a.example.com", "*"),
		conn("publisher", "b.example.com", "*"), // repeat
	)
	outbound := nd(
		conn("partner", "a.example.com", "50980"), // already seen inbound
		conn("partner", "c.example.com", "50980"),
	)

	require.Equal(t,
		[]string{"b.example.com", "a.example.com", "c.example.com"},
		remoteFQDNs(inbound, outbound))
}

func TestAddressesFor_SortedDedupedAndUnmasked(t *testing.T) {
	addrs := []netip.Addr{
		netip.MustParseAddr("10.1.0.9"),
		netip.MustParseAddr("10.1.0.2"),
		netip.MustParseAddr("10.1.0.9"), // duplicate
		netip.MustParseAddr("2001:db8::1"),
		netip.MustParseAddr("::ffff:10.1.0.1"), // v4-mapped, unmapped to v4
	}
	// Bare addresses, no mask: the compound path joins them with a port.
	require.Equal(t, []string{"10.1.0.1", "10.1.0.2", "10.1.0.9"}, addressesFor(addrs))
}

func TestAddressesFor_StableAcrossRecordRotation(t *testing.T) {
	a := netip.MustParseAddr("10.1.0.1")
	b := netip.MustParseAddr("10.1.0.2")
	c := netip.MustParseAddr("10.1.0.3")

	// The same answer in three rotations must expand identically, or every poll
	// looks like a membership change.
	first := addressesFor([]netip.Addr{a, b, c})
	require.Equal(t, first, addressesFor([]netip.Addr{b, c, a}))
	require.Equal(t, first, addressesFor([]netip.Addr{c, a, b}))
}

func TestExpandFQDNs_OneEndpointPerResolvedAddress(t *testing.T) {
	in := nd(
		conn("publisher", "peer.example.com", "*"),
		conn("partner", "10.2.0.1", "*"),
	)
	byName := map[string][]string{"peer.example.com": {"10.1.0.1", "10.1.0.2"}}

	require.Equal(t, nd(
		conn("publisher", "10.1.0.1", "*"),
		conn("publisher", "10.1.0.2", "*"),
		conn("partner", "10.2.0.1", "*"),
	), expandFQDNs(in, byName))
}

func TestExpandFQDNs_PreservesEveryFieldButTheAddress(t *testing.T) {
	in := nd(NetworkConnection{
		Local:       Endpoint{Address: "10.0.0.5", Port: "40840"},
		Remote:      Endpoint{Address: "peer.example.com", Port: "50980"},
		Category:    "partner",
		TLSRequired: true,
	})

	got := expandFQDNs(in, map[string][]string{"peer.example.com": {"10.1.0.1"}})

	require.Len(t, got.ActiveEndpoints, 1)
	require.Equal(t, NetworkConnection{
		Local:       Endpoint{Address: "10.0.0.5", Port: "40840"},
		Remote:      Endpoint{Address: "10.1.0.1", Port: "50980"},
		Category:    "partner",
		TLSRequired: true,
	}, got.ActiveEndpoints[0])
}

func TestExpandFQDNs_DropsEndpointWhoseNameResolvedToNothing(t *testing.T) {
	in := nd(
		conn("publisher", "gone.example.com", "*"),
		conn("partner", "10.2.0.1", "*"),
	)
	byName := map[string][]string{"gone.example.com": nil}

	// The unresolved peer is skipped; the literal beside it still reconciles.
	require.Equal(t, nd(conn("partner", "10.2.0.1", "*")), expandFQDNs(in, byName))
}

func TestExpandFQDNs_CollapsesIdenticalConnections(t *testing.T) {
	// Two endpoints behind one name, plus a literal the name also resolves to:
	// all three would otherwise put 10.1.0.1 in the same set within one nft
	// transaction.
	in := nd(
		conn("publisher", "peer.example.com", "*"),
		conn("publisher", "peer.example.com", "*"),
		conn("publisher", "10.1.0.1", "*"),
	)
	byName := map[string][]string{"peer.example.com": {"10.1.0.1"}}

	require.Equal(t, nd(conn("publisher", "10.1.0.1", "*")), expandFQDNs(in, byName))
}

func TestExpandFQDNs_LeavesStatuszWildcardsAlone(t *testing.T) {
	in := nd(
		inconn("subscriber", "40840"), // remote address "*"
		conn("public", "", ""),
	)
	require.Equal(t, in, expandFQDNs(in, map[string][]string{}))
}

func TestExpandFQDNs_NoNamesReturnsPayloadUnchanged(t *testing.T) {
	in := nd(conn("publisher", "10.1.0.1", "*"), conn("partner", "10.2.0.0/24", "*"))
	require.Equal(t, in, expandFQDNs(in, map[string][]string{}))
}

func TestResolveHosts_ReportsUnresolvedInDiscoveryOrder(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{
		"ok.example.com": {"10.1.0.1"},
	}}

	res := resolveHosts(context.Background(), r,
		[]string{"first.example.com", "ok.example.com", "second.example.com"}, dnsCache{}, resolveTimeout)

	require.Equal(t, []string{"10.1.0.1"}, res.byName["ok.example.com"])
	require.Nil(t, res.byName["first.example.com"])
	require.Equal(t, []string{"first.example.com", "second.example.com"}, res.unresolved)
	// Every name is present in byName, so the expansion can tell "resolved to
	// nothing" from "never asked".
	require.Len(t, res.byName, 3)
}

func TestResolveHosts_NoNamesDoesNotTouchTheResolver(t *testing.T) {
	r := &fakeResolver{}
	res := resolveHosts(context.Background(), r, nil, dnsCache{}, resolveTimeout)
	require.Empty(t, res.byName)
	require.Empty(t, r.calls)
}

func TestResolveHosts_AAAAOnlyContributesNothingButIsNotUnresolved(t *testing.T) {
	r := &fakeResolver{
		answersV6: map[string][]string{"v6only.example.com": {"2001:db8::1"}},
	}

	res := resolveHosts(context.Background(), r, []string{"v6only.example.com"}, dnsCache{}, resolveTimeout)

	require.Nil(t, res.byName["v6only.example.com"])
	require.Equal(t, []string{"v6only.example.com"}, res.aaaaOnly)
	require.Empty(t, res.unresolved)
	require.False(t, res.cacheDirty, "an AAAA-only name is never cached")
}

func TestResolveHosts_FallsBackToCacheOnFailure(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{}}
	cache := dnsCache{"stale.example.com": {Addresses: map[string]time.Time{"10.5.5.5": time.Now().UTC()}}}

	res := resolveHosts(context.Background(), r, []string{"stale.example.com"}, cache, resolveTimeout)

	require.Equal(t, []string{"10.5.5.5"}, res.byName["stale.example.com"])
	require.Equal(t, []string{"stale.example.com"}, res.stale)
	require.Empty(t, res.unresolved)
	require.True(t, res.cacheDirty, "touch re-stamps the cached address")
}

func TestResolveHosts_PrunesNamesNoLongerReported(t *testing.T) {
	r := &fakeResolver{}
	cache := dnsCache{"gone.example.com": {Addresses: map[string]time.Time{"10.5.5.5": time.Now().UTC()}}}

	res := resolveHosts(context.Background(), r, nil, cache, resolveTimeout)

	require.Empty(t, cache, "a name absent from this tick's roster is pruned from the cache")
	require.True(t, res.cacheDirty)
}

func TestResolveHosts_IsCaseInsensitiveAsACacheKey(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{}}
	cache := dnsCache{"peer.example.com": {Addresses: map[string]time.Time{"10.5.5.5": time.Now().UTC()}}}

	// A spelling change between polls (statusz re-cases the name) must still hit
	// the same cache entry.
	res := resolveHosts(context.Background(), r, []string{"Peer.Example.Com"}, cache, resolveTimeout)

	require.Equal(t, []string{"10.5.5.5"}, res.byName["Peer.Example.Com"])
	require.Equal(t, []string{"Peer.Example.Com"}, res.stale)
}

func TestResolveRemotes_ResolvesBothPayloadsInOnePass(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{
		"peer.example.com": {"10.1.0.1", "10.1.0.2"},
	}}
	rec := &Reconciler{resolver: r}

	// The same name on both rosters must cost one lookup and cannot resolve two
	// different ways within a tick.
	inbound, outbound, res := rec.resolveRemotes(context.Background(),
		nd(conn("publisher", "peer.example.com", "*")),
		nd(conn("partner", "peer.example.com", "50980")),
		dnsCache{}, resolveTimeout)

	require.Equal(t, []string{"peer.example.com"}, r.calls)
	require.Empty(t, res.unresolved)
	require.Equal(t, nd(
		conn("publisher", "10.1.0.1", "*"),
		conn("publisher", "10.1.0.2", "*"),
	), inbound)
	require.Equal(t, nd(
		conn("partner", "10.1.0.1", "50980"),
		conn("partner", "10.1.0.2", "50980"),
	), outbound)
}

func TestResolveRemotes_UnresolvableNameDoesNotFail(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{}}
	rec := &Reconciler{resolver: r}

	// Returning an error here would exit the worker non-zero, which faults the
	// daemon's poll loop and retries the same name on a backoff forever.
	inbound, outbound, res := rec.resolveRemotes(context.Background(),
		nd(conn("publisher", "gone.example.com", "*"), conn("partner", "10.2.0.1", "*")),
		nd(), dnsCache{}, resolveTimeout)

	require.Equal(t, nd(conn("partner", "10.2.0.1", "*")), inbound)
	require.Empty(t, outbound.ActiveEndpoints)
	// Returned, never logged: under --output json a log line lands on stdout and
	// corrupts the digest document the daemon parses.
	require.Equal(t, []string{"gone.example.com"}, res.unresolved)
}

// shrinkResolveTimeout keeps the hang cases from waiting out a real deadline,
// on both the apply-path and check-path budgets.
func shrinkResolveTimeout(t *testing.T) {
	t.Helper()
	prevApply, prevCheck := resolveTimeout, checkResolveTimeout
	resolveTimeout = 50 * time.Millisecond
	checkResolveTimeout = 50 * time.Millisecond
	t.Cleanup(func() { resolveTimeout, checkResolveTimeout = prevApply, prevCheck })
}

func TestResolveHosts_OneHangingNameDoesNotFailTheOthers(t *testing.T) {
	shrinkResolveTimeout(t)
	r := &fakeResolver{
		answers: map[string][]string{
			"slow.example.com": {"10.9.9.9"},
			"fast.example.com": {"10.1.0.1"},
		},
		hang: map[string]bool{"slow.example.com": true},
	}

	// A shared pass deadline cancels every in-flight lookup when it expires, so
	// one unanswered name takes down the names beside it. Each lookup owns its
	// own deadline instead.
	res := resolveHosts(context.Background(), r,
		[]string{"slow.example.com", "fast.example.com"}, dnsCache{}, resolveTimeout)

	require.Equal(t, []string{"10.1.0.1"}, res.byName["fast.example.com"],
		"a name that answered must survive a sibling that never did")
	require.Equal(t, []string{"slow.example.com"}, res.unresolved)
}

func TestResolveRemotes_UnresolvedPeerDoesNotDisturbOtherPolicies(t *testing.T) {
	shrinkResolveTimeout(t)
	r := &fakeResolver{
		answers: map[string][]string{"ok.example.com": {"10.1.0.1"}},
		hang:    map[string]bool{"dead.example.com": true},
	}
	rec := &Reconciler{resolver: r}

	inbound, _, res := rec.resolveRemotes(context.Background(),
		nd(
			conn("publisher", "dead.example.com", "*"),
			conn("partner", "ok.example.com", "*"),
			conn("restricted", "10.2.0.1", "*"),
		),
		nd(), dnsCache{}, resolveTimeout)

	require.Equal(t, []string{"dead.example.com"}, res.unresolved)
	require.Equal(t, nd(
		conn("partner", "10.1.0.1", "*"),
		conn("restricted", "10.2.0.1", "*"),
	), inbound, "only the unresolved peer is dropped")
}

func TestCheck_UnresolvedPeerKeepsItsPolicyListenerPort(t *testing.T) {
	shrinkResolveTimeout(t)

	// One inbound publisher endpoint whose remote is a name that will not
	// resolve. local.port is the BLOCK NODE's listener, so it has nothing to do
	// with whether the peer on the other end can be looked up.
	rec := &Reconciler{
		fetcher: &fakeFetcher{inbound: nd(NetworkConnection{
			Local:    Endpoint{Address: "10.0.0.5", Port: "40840"},
			Remote:   Endpoint{Address: "dead.example.com", Port: "*"},
			Category: "publisher",
		})},
		resolver: &fakeResolver{hang: map[string]bool{"dead.example.com": true}},
	}

	res, err := rec.Check(context.Background())
	require.NoError(t, err)

	require.Equal(t, []NamedIssue{{Name: "dead.example.com", Policies: []string{"bn-publisher"}}}, res.Unresolved)
	require.Empty(t, res.Desired["bn-publisher"],
		"the peer itself cannot be classified — that part is expected")
	require.Equal(t, []string{"40840"}, res.DesiredPorts["bn-publisher"],
		"the listener port must survive: deriving it from the resolved payload lets an "+
			"unreachable peer empty <policy>_ports and stop the rule matching at all")
}
