// SPDX-License-Identifier: Apache-2.0

package firewall

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

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// resolveTimeout bounds one whole resolution pass, not one name. It is
// deliberately short because the pass runs inside the cross-command apply flock,
// which the traffic-shaper daemon also contends for — with a non-blocking
// acquire, so every second held here is a daemon reconcile tick skipped.
const resolveTimeout = 2 * time.Second

// addrGracePeriod is how long one address stays in a name's rendered set after
// answers stop mentioning it. It is an idle timer per address, not a TTL: an
// address that appears in every answer never ages out, however long it has been
// cached.
//
// It exists because a resolver may answer with a subset of a name's records --
// geo-steering, load balancers, truncation -- and taking each answer as the whole
// truth makes the rendered set flap. Under a default-drop input chain that is an
// operator locked out of SSH on a new connection, intermittently.
//
// Six refreshes at the timer's five-minute interval. Deliberately a constant and
// not a flag: a value too short reintroduces the flapping this exists to prevent,
// and nothing an operator can observe would tell them so.
const addrGracePeriod = 30 * time.Minute

// Resolver looks up the IPv4 addresses of a domain name. It is an interface for
// the same reason Runner is: the package must build and test on any platform,
// against fixed answers, without touching the host's resolver.
//
// Implementations must be safe for concurrent use — resolveFQDNs looks every
// name up at once, so that a list of names costs one round trip of wall time
// rather than N.
type Resolver interface {
	LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error)
}

// netResolver is the production Resolver, backed by the standard library.
//
// Stdlib rather than a DNS library because the MVP polls on a fixed interval
// and so needs no TTL — which is the only thing net.Resolver cannot report.
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

// dnsCachePathFor names the resolution cache for a given config path, replacing
// the config's extension rather than appending to it — so the production pair is
// `…-host-firewall.yaml` and `…-host-firewall.dns.json`, and a Manager wired to
// a temp config in a test caches beside it instead of writing to /etc.
func dnsCachePathFor(configPath string) string {
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + DNSCacheSuffix
}

// dnsCacheEntry is one name's known addresses, each carrying the last time an
// answer vouched for it.
//
// Per address rather than per answer, because the two things the cache has to
// survive are different: a name failing to resolve at all, and a name resolving
// to less than it did before. A single list plus a single timestamp can only
// express the first.
type dnsCacheEntry struct {
	Addresses map[string]time.Time `json:"addresses"`
}

// UnmarshalJSON reads either shape: the current per-address map, or the flat
// `ips`/`resolvedAt` list written before addresses decayed independently.
//
// Tolerating the flat shape is what stops an already-provisioned host losing its
// last-known addresses on upgrade -- which, for a name that is not resolving at
// that moment, is the difference between a firewall that applies and one that
// refuses. Every address inherits the answer's single timestamp, so a host
// upgraded mid-outage starts its grace window from the last good answer.
//
// The legacy list holds nft elements (`192.0.2.7/32`); the map is keyed by bare
// addresses, so that elementsFor stays the only place that knows the nft
// encoding.
func (e *dnsCacheEntry) UnmarshalJSON(data []byte) error {
	var raw struct {
		Addresses  map[string]time.Time `json:"addresses"`
		IPs        []string             `json:"ips"`
		ResolvedAt time.Time            `json:"resolvedAt"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Addresses != nil {
		e.Addresses = raw.Addresses
		return nil
	}
	e.Addresses = make(map[string]time.Time, len(raw.IPs))
	for _, element := range raw.IPs {
		addr, _, _ := strings.Cut(element, "/")
		e.Addresses[addr] = raw.ResolvedAt
	}
	return nil
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
// No answer is not evidence an address is gone. Letting the clock run through an
// outage would mean the first partial answer after it recovers finds everything
// aged out and prunes it in one pass -- the flap this mechanism exists to
// prevent, relocated to the moment of recovery, which is exactly when an
// operator is likely to be reconnecting.
func (e *dnsCacheEntry) touch(now time.Time) {
	for a := range e.Addresses {
		e.Addresses[a] = now
	}
}

// decay drops addresses no answer has vouched for within addrGracePeriod,
// reporting whether it removed any.
//
// It cannot empty a populated entry: mergeAnswer only ever runs on a non-empty
// answer and stamps those addresses with now, and touch stamps all of them, so
// every path into decay leaves at least one address inside the window.
func (e *dnsCacheEntry) decay(now time.Time) bool {
	dropped := false
	for a, seen := range e.Addresses {
		if now.Sub(seen) > addrGracePeriod {
			delete(e.Addresses, a)
			dropped = true
		}
	}
	return dropped
}

// elements renders the entry as nft set elements. Unparseable keys -- only
// reachable by hand-editing the file -- are skipped rather than failing the
// apply, on the same reasoning as loadDNSCache.
func (e dnsCacheEntry) elements() []string {
	addrs := make([]netip.Addr, 0, len(e.Addresses))
	for s := range e.Addresses {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		addrs = append(addrs, a)
	}
	return elementsFor(addrs)
}

// dnsCache maps a normalised FQDN to the addresses it has recently resolved to.
//
// It exists for exactly one job: attribution. Without it, "on resolution
// failure keep the last-known addresses" is only well defined when every name
// fails at once. When one name fails while another rotates, recovering the
// failed name's addresses means knowing which members of the shared mgmt_addrs
// set came from it — and a merged set cannot say. Falling back to the rendered
// artifact instead would pin the rotated name's old address forever; dropping
// the failed name would silently narrow the allowlist.
//
// It is never the source of truth. The YAML holds the names; this holds only
// what they last pointed at, and a missing or unreadable file degrades to
// "no last-known addresses" rather than failing the command.
type dnsCache map[string]dnsCacheEntry

// loadDNSCache reads the cache, returning an empty one for any problem at all.
// A cache that cannot be read is indistinguishable from a cold start, and
// neither is a reason to refuse to apply a firewall.
func loadDNSCache(path string) dnsCache {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logx.As().Warn().Err(err).Str("path", path).Msg(
				"could not read the DNS resolution cache; continuing without last-known addresses")
		}
		return dnsCache{}
	}
	var c dnsCache
	if err := json.Unmarshal(data, &c); err != nil {
		logx.As().Warn().Err(err).Str("path", path).Msg(
			"the DNS resolution cache is not valid JSON; continuing without last-known addresses")
		return dnsCache{}
	}
	if c == nil {
		return dnsCache{}
	}
	return c
}

// save writes the cache atomically. A failure here is logged and swallowed: the
// firewall it describes has already been applied, and refusing the whole verb
// because a fallback file could not be written would be the worse outcome.
func (c dnsCache) save(path string) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		logx.As().Warn().Err(err).Msg("could not encode the DNS resolution cache")
		return
	}
	if err := atomicWriteFile(path, string(data)+"\n", 0o600); err != nil {
		logx.As().Warn().Err(err).Str("path", path).Msg(
			"could not write the DNS resolution cache; a later resolver outage will have no last-known addresses to fall back on")
	}
}

// resolution is the outcome of one pass over a table's FQDN entries.
type resolution struct {
	// byName holds an entry for every FQDN in the table, mapping it to its
	// address elements in nft form. A name that produced neither a fresh answer
	// nor a cached one maps to an empty slice — present, contributing nothing —
	// so expandFQDNs can tell "resolved to nothing" from "never asked".
	byName map[string][]string
	// touched is true when the pass changed any name's cache entry -- a fresh
	// answer, a re-stamp of what the fallback kept, or a decayed address -- which
	// is what makes the cache worth rewriting.
	touched bool
	// stale names were served from the cache after the resolver declined.
	stale []string
	// missing names had neither a fresh answer nor a cached one.
	missing []string
}

// fqdnEntries returns the distinct FQDN entries across every rule that accepts
// them, in render order. See Rule.acceptsFQDN.
//
// Driven off Table.rules rather than naming the fields it walks, so widening
// acceptsFQDN is the only edit a newly name-accepting rule needs.
func (t *Table) fqdnEntries() []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range t.rules() {
		if !r.acceptsFQDN() {
			continue
		}
		for _, c := range r.CIDRs {
			if sanity.IsFQDNEntry(c) && !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// ruleFQDNs returns the FQDN entries within one rule's own CIDR list — used to
// scope a table-wide missing/stale list down to the names a specific rule's
// error or warning should mention, rather than every failed name table-wide.
func ruleFQDNs(r *Rule) []string {
	var out []string
	for _, c := range r.CIDRs {
		if sanity.IsFQDNEntry(c) {
			out = append(out, c)
		}
	}
	return out
}

// intersect returns the elements of names that also appear in missing,
// preserving missing's order — the order resolveFQDNs discovered them in.
func intersect(names, missing []string) []string {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	var out []string
	for _, m := range missing {
		if set[m] {
			out = append(out, m)
		}
	}
	return out
}

// resolveFQDNs resolves every FQDN in the table, preferring a fresh answer and
// falling back to the cache per name.
//
// Per name, not per pass: that is the whole point of the cache. One name being
// unreachable must not freeze the answers for the others, and one name rotating
// must not resurrect another's stale address.
func (m *Manager) resolveFQDNs(ctx context.Context, t *Table) *resolution {
	names := t.fqdnEntries()
	res := &resolution{byName: make(map[string][]string, len(names))}
	if len(names) == 0 {
		return res
	}

	cache := loadDNSCache(m.dnsCachePath)
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	// Concurrently, because resolveTimeout budgets the whole pass rather than
	// each name: sequentially, a handful of names behind a slow resolver would
	// exhaust it and report every name after the first few as unresolvable. Each
	// goroutine owns one slot, so no locking is needed.
	answers := make([]struct {
		addrs []netip.Addr
		err   error
	}, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i].addrs, answers[i].err = m.resolver.LookupIPv4(ctx, name)
		}()
	}
	wg.Wait()

	// One timestamp for the whole pass, so that names resolved concurrently decay
	// on the same clock and a test can reason about a pass rather than about the
	// order goroutines happened to finish in.
	now := time.Now().UTC()

	// Folded back in list order, not completion order, so the warnings and the
	// error message name things in the order the operator wrote them.
	for i, name := range names {
		entry := cache[name]
		if answers[i].err == nil && len(answers[i].addrs) > 0 {
			// The union of what the name has recently answered, not just this
			// answer: see addrGracePeriod.
			entry.mergeAnswer(answers[i].addrs, now)
			entry.decay(now)
			cache[name] = entry
			res.byName[name] = entry.elements()
			res.touched = true
			continue
		}
		if len(entry.Addresses) > 0 {
			entry.touch(now)
			cache[name] = entry
			res.byName[name] = entry.elements()
			res.stale = append(res.stale, name)
			res.touched = true
			continue
		}
		res.byName[name] = nil
		res.missing = append(res.missing, name)
	}

	// Drop names the operator has since removed, so the file does not grow
	// forever with hosts this firewall no longer mentions.
	pruned := false
	for name := range cache {
		if _, ok := res.byName[name]; !ok {
			delete(cache, name)
			pruned = true
		}
	}
	// Persist on any entry change OR a prune: a prune with the resolver down must
	// still reach disk, or a name removed from the config during an outage
	// lingers in the cache until the resolver happens to succeed again.
	if res.touched || pruned {
		cache.save(m.dnsCachePath)
	}

	if len(res.stale) > 0 {
		logx.As().Warn().Strs("fqdns", res.stale).Msg(
			"could not resolve these host firewall entries; keeping their last-known addresses")
		// Called out separately for the rules where holding a stale answer is a
		// gap rather than a safe default: the entry still denies the address it
		// last named, so if the host has moved, its current address is not
		// covered and nothing else says so.
		for _, r := range t.rules() {
			if !r.unresolvedFailsOpen() {
				continue
			}
			if own := intersect(ruleFQDNs(r), res.stale); len(own) > 0 {
				logx.As().Warn().Strs("fqdns", own).Str("rule", r.Name).Msg(
					"these entries are still enforced at their last-known addresses; if the hosts they name have " +
						"moved since, their current addresses are not covered")
			}
		}
	}
	return res
}

// elementsFor turns a resolver answer into nft set elements: deduped, /32'd, and
// sorted.
//
// Sorting is not cosmetic. DNS rotates the record order between answers, so an
// unsorted list makes every single refresh look like a membership change and
// re-render the whole table for nothing.
func elementsFor(addrs []netip.Addr) []string {
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
		out = append(out, a.String()+"/32")
	}
	return out
}

// expandFQDNs returns a copy of the table with every FQDN, in every rule that
// accepts them, replaced by its resolved addresses, leaving the receiver
// untouched.
//
// A copy, not an in-place rewrite, because the two outputs of an apply need
// different views of the same table: the YAML records what the operator wrote
// and must keep the name, while the nft document must carry only literals. Doing
// this by construction rather than by a guard inside Render is what makes it
// impossible to emit a document containing a bare name — which matters, because
// nft would then resolve it itself at load time, silently bypassing this
// resolver, the cache fallback, and the never-empty rule below.
//
// out.Allow is re-sliced onto a fresh backing array before any element is
// mutated: `out := *t` shares Allow's backing array with the receiver, so
// writing through out.Allow[i] without this would corrupt CIDRs the caller
// still holds a reference to.
func (t *Table) expandFQDNs(byName map[string][]string) (*Table, error) {
	if len(byName) == 0 {
		return t, nil
	}

	out := *t
	out.Allow = append([]Rule(nil), t.Allow...)

	for _, r := range out.rules() {
		if !r.acceptsFQDN() {
			continue
		}
		cidrs := make([]string, 0, len(r.CIDRs))
		for _, c := range r.CIDRs {
			if !sanity.IsFQDNEntry(c) {
				cidrs = append(cidrs, c)
				continue
			}
			elements, ok := byName[c]
			if !ok {
				return nil, errorx.AssertionFailed.New(
					"%q was not resolved before rendering; refusing to render an address list that omits it", c)
			}
			cidrs = append(cidrs, elements...)
		}
		// A fresh slice, never an in-place write: the shallow copy above shares
		// every rule's CIDRs backing array with the receiver.
		r.CIDRs = sortedDedupe(cidrs)
	}
	return &out, nil
}

// checkResolvedRule refuses to render a rule that was populated (with at least
// one entry) but that resolution left holding nothing to match, when the rule
// says an empty result is unacceptable (Rule.mustResolveToSomething). Refusal
// is a property of the rule itself, not of this function — mgmt is a caller
// like any other, not a special case in this body.
//
// Distinct from checkMgmtLockout, which compares an explicit mutation's before
// and after: this catches a rule that is entirely domain names when none of
// them resolve and none has ever been cached. There is no --force past this:
// an empty result is a lock-out however it was arrived at.
func checkResolvedRule(before, after *Rule, missing []string) error {
	if len(before.CIDRs) == 0 || len(after.CIDRs) > 0 || !before.mustResolveToSomething() {
		return nil
	}
	ownMissing := intersect(ruleFQDNs(before), missing)
	return errx.Decorate(
		errorx.IllegalState.New(
			"%q holds only domain names and none of them resolved (%v), so @%s would render empty — which, under "+
				"this host's default-drop policy, silently withdraws everything the rule was meant to admit. "+
				"Nothing was changed", before.Name, ownMissing, addrSetName(before.Name)),
		reasons.PreconditionNotMet,
		"Check name resolution on this host: `getent hosts "+firstOr(ownMissing, "<name>")+"`",
		"Add a literal address alongside the names: `solo-provisioner network firewall add --name "+before.Name+" --cidr <cidr>`")
}

// checkFailOpenRules refuses to render whenever a rule that fails open has a
// name resolution could not answer at all — neither freshly nor from the cache.
//
// Unlike checkResolvedRule this does not wait for the rule to render empty. One
// name out of ten going missing from the block list is already the failure: that
// host is reachable again, and the other nine entries make the rule look healthy.
//
// It also ignores applyOpts.tolerateUnresolved, which is what separates this
// from every other unresolved-name path. Tolerance exists so that a resolver
// outage cannot stop an operator re-asserting their firewall, and so that the
// five-minute timer does not fail on a blip — but both of those arguments are
// about *losing access*, and here the same event grants it. Refusing writes
// nothing, so the live ruleset keeps dropping what it was dropping; that is the
// safe direction.
//
// A name that has resolved even once is cached indefinitely, so on a working
// host this fires only for a name that has never resolved (a typo, caught at
// add time) or after the cache file has been lost.
func checkFailOpenRules(t *Table, missing []string) error {
	if len(missing) == 0 {
		return nil
	}
	for _, r := range t.rules() {
		if !r.unresolvedFailsOpen() {
			continue
		}
		own := intersect(ruleFQDNs(r), missing)
		if len(own) == 0 {
			continue
		}
		return errx.Decorate(
			errorx.IllegalState.New(
				"%v in %q resolved to no address and none is on record, so rendering would drop them from @%s and "+
					"silently stop blocking the hosts they name. Nothing was changed", own, r.Name, addrSetName(r.Name)),
			reasons.PreconditionNotMet,
			"Check name resolution on this host: `getent hosts "+firstOr(own, "<name>")+"`",
			"Remove the entry if the host is gone: `solo-provisioner network firewall remove --name "+r.Name+
				" --cidr "+firstOr(own, "<name>")+"`")
	}
	return nil
}

// firstOr returns the first element of s, or fallback when s is empty, so an
// error hint can quote a real name instead of a placeholder.
func firstOr(s []string, fallback string) string {
	if len(s) > 0 {
		return s[0]
	}
	return fallback
}
