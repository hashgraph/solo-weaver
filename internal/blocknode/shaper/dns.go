// SPDX-License-Identifier: Apache-2.0

package shaper

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joomcode/errorx"
)

// resolveTimeout bounds one whole resolution pass, not one name. The budget is
// per tick rather than per lookup because the privileged half of a reconcile
// spends its time holding the shared apply flock that operator commands contend
// for, so a roster of slow names must not extend that hold in proportion to its
// length.
const resolveTimeout = 2 * time.Second

// Resolver looks up the IPv4 addresses of a host name. It is an interface so a
// reconcile can be driven against fixed answers without touching the host's
// resolver.
//
// Implementations must be safe for concurrent use: resolveHosts looks every name
// up at once, so a roster of names costs one round trip of wall time rather
// than N.
type Resolver interface {
	LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error)
}

// netResolver is the production Resolver, backed by the standard library.
type netResolver struct{ r *net.Resolver }

// NewNetResolver returns the production Resolver.
func NewNetResolver() Resolver { return &netResolver{r: net.DefaultResolver} }

func (n *netResolver) LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error) {
	addrs, err := n.r.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to resolve %s", host)
	}
	return addrs, nil
}

// hostResolution is the outcome of one pass over the names in a statusz payload.
type hostResolution struct {
	// byName holds an entry for every name asked about. A name that produced no
	// answer maps to an empty slice — present, contributing nothing — so the
	// expansion can tell "resolved to nothing" from "never asked".
	byName map[string][]string
	// unresolved names produced no answer this pass, in the order they were
	// discovered.
	unresolved []string
}

// isRemoteFQDN reports whether a statusz remote address is a domain name this
// pass should resolve.
//
// The host firewall's sanity.IsFQDNEntry does not fit here. It classifies
// anything that is neither an IP nor masked as a name, which is right for
// operator input but wrong for statusz: that vocabulary also carries "" and "*"
// for the fallthrough and any-address entries, and both would be handed to the
// resolver. So the test is positive — it must look like a name — rather than
// "not a literal". A value that is neither a literal nor a name flows through
// untouched, exactly as it does today.
//
// A dot is required, so a single-label value is not resolved. Resolving one
// would make the result depend on the host's search domain, which means two
// nodes reading the same roster could classify a peer differently.
func isRemoteFQDN(addr string) bool {
	if !strings.Contains(addr, ".") || net.ParseIP(addr) != nil {
		return false
	}
	for _, c := range addr {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-':
		default:
			return false
		}
	}
	return true
}

// remoteFQDNs returns the distinct domain names appearing as a remote address
// across the given payloads, in first-seen order.
func remoteFQDNs(datas ...NetworkData) []string {
	var out []string
	seen := map[string]bool{}
	for _, nd := range datas {
		for _, conn := range nd.ActiveEndpoints {
			addr := conn.Remote.Address
			if isRemoteFQDN(addr) && !seen[addr] {
				seen[addr] = true
				out = append(out, addr)
			}
		}
	}
	return out
}

// resolveHosts resolves every name in one concurrent pass under a single
// deadline.
//
// Concurrent because resolveTimeout budgets the whole pass: sequentially, a
// handful of names behind a slow resolver would exhaust it and report every name
// after the first few as unresolvable. Each goroutine owns one slot, so no
// locking is needed.
func resolveHosts(ctx context.Context, resolver Resolver, names []string) *hostResolution {
	res := &hostResolution{byName: make(map[string][]string, len(names))}
	if len(names) == 0 {
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	answers := make([]struct {
		addrs []netip.Addr
		err   error
	}, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i].addrs, answers[i].err = resolver.LookupIPv4(ctx, name)
		}()
	}
	wg.Wait()

	// Folded back in discovery order, not completion order, so logs name things
	// in the order statusz reported them.
	for i, name := range names {
		if answers[i].err == nil && len(answers[i].addrs) > 0 {
			res.byName[name] = addressesFor(answers[i].addrs)
			continue
		}
		res.byName[name] = nil
		res.unresolved = append(res.unresolved, name)
	}
	return res
}

// addressesFor turns a resolver answer into sorted, deduplicated bare IPv4
// address strings.
//
// Bare addresses rather than /32s: the endpoints they expand into are read back
// by hostCIDR, which applies the mask for the plain-CIDR sets, and by
// net.JoinHostPort, which requires an unmasked host for the compound set. A mask
// applied here would break the compound path.
//
// Sorting is not cosmetic. DNS rotates record order between answers, so an
// unsorted expansion makes every poll look like a membership change and rewrites
// the sets — and the persisted artifact — for nothing.
func addressesFor(addrs []netip.Addr) []string {
	uniq := make([]netip.Addr, 0, len(addrs))
	seen := map[netip.Addr]bool{}
	for _, a := range addrs {
		a = a.Unmap()
		if !a.Is4() || seen[a] {
			continue
		}
		seen[a] = true
		uniq = append(uniq, a)
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i].Compare(uniq[j]) < 0 })

	out := make([]string, 0, len(uniq))
	for _, a := range uniq {
		out = append(out, a.String())
	}
	return out
}

// expandFQDNs returns a copy of nd with every endpoint whose remote address is a
// name replaced by one endpoint per address it resolved to, leaving the receiver
// untouched. An endpoint whose name resolved to nothing is dropped.
//
// Identical connections are collapsed. One name behind two endpoints of the same
// category, or a name resolving to an address a second endpoint already reports
// literally, would otherwise put the same element in a set twice within one
// `nft` transaction. Literals can collide the same way, so the dedupe is on the
// whole connection rather than only on the endpoints this pass rewrote.
func expandFQDNs(nd NetworkData, byName map[string][]string) NetworkData {
	out := make([]NetworkConnection, 0, len(nd.ActiveEndpoints))
	seen := make(map[NetworkConnection]bool, len(nd.ActiveEndpoints))

	keep := func(conn NetworkConnection) {
		if seen[conn] {
			return
		}
		seen[conn] = true
		out = append(out, conn)
	}

	for _, conn := range nd.ActiveEndpoints {
		if !isRemoteFQDN(conn.Remote.Address) {
			keep(conn)
			continue
		}
		for _, addr := range byName[conn.Remote.Address] {
			expanded := conn
			expanded.Remote.Address = addr
			keep(expanded)
		}
	}
	return NetworkData{ActiveEndpoints: out}
}

// resolveRemotes replaces every domain name in the two payloads with the
// addresses it resolves to, returning resolved copies plus the names that
// produced no answer.
//
// Both payloads go through one pass so a name reported on both the inbound and
// outbound rosters costs a single lookup and cannot resolve two different ways
// within a tick.
//
// It never fails. A name that does not resolve contributes nothing and its
// endpoint is dropped, because the alternative — returning an error — exits the
// worker non-zero, which faults the daemon's poll loop and retries the same
// unresolvable name on a backoff forever.
//
// The unresolved names are RETURNED rather than logged. Under --output json the
// root command routes every log line to stdout as NDJSON, which is the same
// stream the digest is written to and the daemon parses — so a log line here
// makes the daemon's json.Unmarshal fail and faults the poll loop just as surely
// as a non-zero exit would. The caller folds them into its result instead, where
// they ride the one JSON document the contract allows.
func (r *Reconciler) resolveRemotes(ctx context.Context, inbound, outbound NetworkData) (NetworkData, NetworkData, []string) {
	names := remoteFQDNs(inbound, outbound)
	if len(names) == 0 {
		return inbound, outbound, nil
	}

	res := resolveHosts(ctx, r.resolver, names)
	return expandFQDNs(inbound, res.byName), expandFQDNs(outbound, res.byName), res.unresolved
}
