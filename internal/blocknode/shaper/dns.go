// SPDX-License-Identifier: Apache-2.0

package shaper

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
)

// resolveTimeout bounds one resolution pass on the privileged apply path.
//
// The bound exists because a reconcile tick is a scheduled unit of work, NOT
// because of lock contention: resolution finishes inside fetchEndpoints, well
// before ApplySets takes the shared apply flock. Two seconds is short against a
// default resolv.conf, where `timeout:5 attempts:2` lets a single dropped query
// run to ten seconds -- but a name that misses this budget now has the cache
// (see dnsCache) to fall back on, so a slow resolver costs a stale answer
// rather than an unclassified peer.
//
// A var, not a const, only so tests can shrink it and avoid waiting out a real
// deadline; production never reassigns it.
var resolveTimeout = 2 * time.Second

// checkResolveTimeout bounds the unprivileged --check path's resolution pass,
// separately from resolveTimeout. --check takes no lock at all, so nothing else
// is waiting on it the way the apply path's scheduled tick is; there is no
// reason to pay the apply path's tight budget here and manufacture a spurious
// unresolved/stale report out of a resolver that is merely slow rather than
// down. Longer, not unbounded: --check still runs once per poll tick, so it
// must not hang indefinitely on a dead resolver.
var checkResolveTimeout = 5 * time.Second

// addrGracePeriod is how long one address stays in a name's cached entry after
// answers stop mentioning it. It is an idle timer per address, not a TTL: an
// address that appears in every answer never ages out, however long it has
// been cached.
//
// It exists because a resolver may answer with a subset of a name's records --
// geo-steering, load balancers, truncation -- and taking each answer as the
// whole truth makes the rendered set flap. For bn-restricted, a deny policy,
// flapping a quarantined peer's membership means briefly un-quarantining it.
//
// Mirrors the host firewall's addrGracePeriod (internal/network/firewall/dns.go)
// by value; kept as a separate constant here rather than shared, matching how
// the two packages already duplicate their nft path constants (see
// internal/network/policy/paths.go).
const addrGracePeriod = 30 * time.Minute

// dnsCacheSuffix names the on-disk statusz name-resolution cache file,
// replacing policy.WeaverNftPath's own extension the same way the host
// firewall derives its cache path from its config path (see
// internal/network/firewall/dns.go dnsCachePathFor) -- so the cache can never
// drift from the artifact it sits beside.
const dnsCacheSuffix = ".statusz-dns.json"

// dnsCachePathFor names the statusz DNS resolution cache for a given
// policy-plane nft artifact path.
func dnsCachePathFor(nftPath string) string {
	return strings.TrimSuffix(nftPath, filepath.Ext(nftPath)) + dnsCacheSuffix
}

// Resolver looks up a host name's addresses. It is an interface so a reconcile
// can be driven against fixed answers without touching the host's resolver.
//
// Implementations must be safe for concurrent use: resolveHosts looks every
// name up at once, so a roster of names costs one round trip of wall time
// rather than N.
type Resolver interface {
	// LookupIPv4 looks up a name's A records.
	LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error)
	// LookupIPv6 looks up a name's AAAA records. It exists solely to tell "this
	// name has no A records because it is AAAA-only" (a structural fact, never
	// cached or retried) from "this name did not resolve at all" (a failure the
	// cache falls back for) -- resolveHosts only calls it when LookupIPv4 came
	// back empty.
	LookupIPv6(ctx context.Context, host string) ([]netip.Addr, error)
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

func (n *netResolver) LookupIPv6(ctx context.Context, host string) ([]netip.Addr, error) {
	addrs, err := n.r.LookupNetIP(ctx, "ip6", host)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to resolve %s", host)
	}
	return addrs, nil
}

// dnsCacheEntry is one name's known addresses, each carrying the last time an
// answer vouched for it. Per address rather than per answer, because the two
// things the cache has to survive are different: a name failing to resolve at
// all, and a name resolving to less than it did before. A single list plus a
// single timestamp can only express the first.
type dnsCacheEntry struct {
	Addresses map[string]time.Time `json:"addresses"`
}

// mergeAnswer folds a fresh answer in: addresses it names are vouched for as of
// now, addresses it omits keep the timestamp they had and are left to decay.
func (e *dnsCacheEntry) mergeAnswer(addrs []netip.Addr, now time.Time) {
	if e.Addresses == nil {
		e.Addresses = make(map[string]time.Time, len(addrs))
	}
	for _, a := range addrs {
		a = a.Unmap()
		if !a.Is4() {
			continue
		}
		e.Addresses[a.String()] = now
	}
}

// touch vouches for every known address without a fresh answer, for a name the
// resolver did not answer at all.
//
// No answer is not evidence an address is gone. Letting the clock run through
// an outage would mean the first partial answer after it recovers finds
// everything aged out and prunes it in one pass -- the flap this mechanism
// exists to prevent, relocated to the moment of recovery.
func (e *dnsCacheEntry) touch(now time.Time) {
	for a := range e.Addresses {
		e.Addresses[a] = now
	}
}

// decay drops addresses no answer has vouched for within addrGracePeriod.
func (e *dnsCacheEntry) decay(now time.Time) {
	for a, seen := range e.Addresses {
		if now.Sub(seen) >= addrGracePeriod {
			delete(e.Addresses, a)
		}
	}
}

// elements renders the entry as the same bare, sorted, deduped address form
// addressesFor produces from a fresh answer, so a cache-served name and a
// freshly resolved one feed expandFQDNs identically.
func (e dnsCacheEntry) elements() []string {
	addrs := make([]netip.Addr, 0, len(e.Addresses))
	for s := range e.Addresses {
		if a, err := netip.ParseAddr(s); err == nil {
			addrs = append(addrs, a)
		}
	}
	return addressesFor(addrs)
}

// dnsCache maps a normalised (lower-cased) FQDN to its last-known addresses.
//
// It exists for two jobs at once: last-known-good fallback when a name fails
// to resolve, and attribution -- an operator reading this file can see which
// address a name last produced, which the rendered nft set membership alone
// cannot say once several names share a set.
//
// It is never the source of truth: statusz is. A missing or unreadable file
// degrades to "no last-known addresses" rather than failing a tick. It cannot
// be written from --check: that path runs unprivileged and cannot write 0600
// files under /etc/solo-provisioner/, so only Reconciler.Apply ever calls
// save, and only after the kernel write the cache describes has already
// succeeded (see Reconciler.persistDNSCache).
type dnsCache map[string]dnsCacheEntry

// loadDNSCache reads the cache, returning an empty one for any problem at all
// -- a cache that cannot be read is indistinguishable from a cold start, and
// neither is a reason to fail a tick. Silent on error rather than warning,
// unlike the host firewall's equivalent: this package must never log (see
// TestPackageEmitsNoLogs), on the Check path or the Apply path alike.
func loadDNSCache(path string) dnsCache {
	if path == "" {
		return dnsCache{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return dnsCache{}
	}
	var c dnsCache
	if err := json.Unmarshal(data, &c); err != nil || c == nil {
		return dnsCache{}
	}
	return c
}

// save writes the cache atomically, swallowing any failure the same way
// loadDNSCache swallows a read failure: by the time save is called the kernel
// write this cache describes has already committed (see
// Reconciler.persistDNSCache), and there is no log line this package is
// allowed to emit to report the fallback file failed.
//
// 0o644, not 0o600: unlike the host firewall's cache, this one has to be
// READ by the unprivileged --check path, which is the whole reason Check and
// Apply agree on a digest during an outage. A 0o600 file owned by root would
// make --check's loadDNSCache silently see an empty cache forever (permission
// errors degrade the same as "missing" -- see loadDNSCache), so --check would
// report a peer unresolved while Apply, running as root, correctly served it
// stale from the very same file. The content is non-sensitive: names and
// addresses the block node already reports over statusz.
func (c dnsCache) save(path string) {
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = fsx.AtomicWriteFile(path, append(data, '\n'), 0o644)
}

// hostResolution is the outcome of one pass over the names in a statusz
// payload.
type hostResolution struct {
	// byName holds an entry for every name asked about, keyed by its ORIGINAL
	// spelling (not case-folded), because expandFQDNs looks up
	// conn.Remote.Address verbatim. A name that produced no addresses -- never
	// resolved, AAAA-only, or resolved-but-not-cached -- maps to an empty
	// slice: present, contributing nothing, so expandFQDNs can tell "resolved
	// to nothing" from "never asked".
	byName map[string][]string
	// unresolved names produced no fresh answer and had no cached fallback, in
	// discovery order.
	unresolved []string
	// stale names produced no fresh answer but were served their last-known
	// addresses from the cache, in discovery order.
	stale []string
	// aaaaOnly names resolved, but only to AAAA records. This is a structural
	// fact about the name, not a resolution failure: it is never cached and
	// never falls back to a last-known-good answer, because there has never
	// been an IPv4 address to remember.
	aaaaOnly []string
	// cache is what the on-disk cache should become if this pass's apply
	// commits. Written only by Reconciler.persistDNSCache, past the point
	// nothing can undo the kernel write it describes.
	cache dnsCache
	// cacheDirty is true when cache differs from what was loaded from disk.
	cacheDirty bool
}

// resolveHosts resolves every name concurrently against the resolver,
// preferring a fresh answer and falling back to the cache per name.
//
// Per name, not per pass: one name being unreachable must not freeze the
// answers for the others, and one name rotating must not resurrect another
// name's stale address. cache is mutated in place (merged answers, touches,
// decays, prunes) and doubles as the return value's cache field.
func resolveHosts(ctx context.Context, resolver Resolver, names []string, cache dnsCache, budget time.Duration) *hostResolution {
	res := &hostResolution{byName: make(map[string][]string, len(names)), cache: cache}

	// Prune names statusz no longer reports, so the cache does not grow
	// forever. This runs even when names is empty: a roster that drops to
	// nothing must still empty the cache, not leave it to decay one address at
	// a time.
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[strings.ToLower(n)] = true
	}
	for key := range cache {
		if !wanted[key] {
			delete(cache, key)
			res.cacheDirty = true
		}
	}
	if len(names) == 0 {
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	// One goroutine per name, each attempting v4 first and only spending a
	// second lookup on v6 when v4 came back empty -- avoids doubling the
	// resolver load on the common case where v4 succeeds.
	answers := make([]struct {
		v4    []netip.Addr
		v4err error
		v6    []netip.Addr
		v6err error
	}, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			answers[i].v4, answers[i].v4err = resolver.LookupIPv4(ctx, name)
			if answers[i].v4err != nil || len(answers[i].v4) == 0 {
				answers[i].v6, answers[i].v6err = resolver.LookupIPv6(ctx, name)
			}
		}(i, name)
	}
	wg.Wait()

	// One timestamp for the whole pass, so names resolved concurrently decay on
	// the same clock.
	now := time.Now().UTC()

	// Folded back in list order, not completion order, so the daemon's log and
	// the CLI's text output name things in the order statusz reported them.
	for i, name := range names {
		key := strings.ToLower(name)
		if answers[i].v4err == nil && len(answers[i].v4) > 0 {
			entry := cache[key]
			entry.mergeAnswer(answers[i].v4, now)
			entry.decay(now)
			cache[key] = entry
			res.byName[name] = entry.elements()
			res.cacheDirty = true
			continue
		}
		if answers[i].v6err == nil && len(answers[i].v6) > 0 {
			res.byName[name] = nil
			res.aaaaOnly = append(res.aaaaOnly, name)
			continue
		}
		if entry, ok := cache[key]; ok && len(entry.Addresses) > 0 {
			entry.touch(now)
			cache[key] = entry
			res.byName[name] = entry.elements()
			res.stale = append(res.stale, name)
			res.cacheDirty = true
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
// addresses it resolves to (fresh, or last-known-good from cache), returning
// resolved copies plus the pass's hostResolution.
//
// Both payloads go through one pass so a name reported on both the inbound and
// outbound rosters costs a single lookup and cannot resolve two different ways
// within a tick.
//
// It never fails. A name that does not resolve and has no cached fallback
// contributes nothing and its endpoint is dropped, because the alternative --
// returning an error -- exits the worker non-zero, which faults the daemon's
// poll loop and retries the same unresolvable name on a backoff forever.
func (r *Reconciler) resolveRemotes(ctx context.Context, inbound, outbound NetworkData, cache dnsCache, budget time.Duration) (NetworkData, NetworkData, *hostResolution) {
	names := remoteFQDNs(inbound, outbound)
	res := resolveHosts(ctx, r.resolver, names, cache, budget)
	if len(names) == 0 {
		return inbound, outbound, res
	}
	return expandFQDNs(inbound, res.byName), expandFQDNs(outbound, res.byName), res
}
