// SPDX-License-Identifier: Apache-2.0

package shaper

import (
	"context"
	"net/netip"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers from a fixed table. A name mapped to a nil slice is
// treated as unresolvable, so a test can exercise the failure path without a
// resolver outage.
type fakeResolver struct {
	answers map[string][]string
	calls   []string
}

func (f *fakeResolver) LookupIPv4(_ context.Context, host string) ([]netip.Addr, error) {
	f.calls = append(f.calls, host)
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
		[]string{"first.example.com", "ok.example.com", "second.example.com"})

	require.Equal(t, []string{"10.1.0.1"}, res.byName["ok.example.com"])
	require.Nil(t, res.byName["first.example.com"])
	require.Equal(t, []string{"first.example.com", "second.example.com"}, res.unresolved)
	// Every name is present in byName, so the expansion can tell "resolved to
	// nothing" from "never asked".
	require.Len(t, res.byName, 3)
}

func TestResolveHosts_NoNamesDoesNotTouchTheResolver(t *testing.T) {
	r := &fakeResolver{}
	res := resolveHosts(context.Background(), r, nil)
	require.Empty(t, res.byName)
	require.Empty(t, r.calls)
}

func TestResolveRemotes_ResolvesBothPayloadsInOnePass(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{
		"peer.example.com": {"10.1.0.1", "10.1.0.2"},
	}}
	rec := &Reconciler{resolver: r}

	// The same name on both rosters must cost one lookup and cannot resolve two
	// different ways within a tick.
	inbound, outbound := rec.resolveRemotes(context.Background(),
		nd(conn("publisher", "peer.example.com", "*")),
		nd(conn("partner", "peer.example.com", "50980")))

	require.Equal(t, []string{"peer.example.com"}, r.calls)
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
	inbound, outbound := rec.resolveRemotes(context.Background(),
		nd(conn("publisher", "gone.example.com", "*"), conn("partner", "10.2.0.1", "*")),
		nd())

	require.Equal(t, nd(conn("partner", "10.2.0.1", "*")), inbound)
	require.Empty(t, outbound.ActiveEndpoints)
}
