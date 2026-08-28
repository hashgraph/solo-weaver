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

// dnsCacheEntry is one name's last successful answer.
type dnsCacheEntry struct {
	IPs        []string  `json:"ips"`
	ResolvedAt time.Time `json:"resolvedAt"`
}

// dnsCache maps a normalised FQDN to its last successful answer.
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
	// fresh is true when at least one name was answered by the resolver this
	// pass, which is what makes the cache worth rewriting.
	fresh bool
	// stale names were served from the cache after the resolver declined.
	stale []string
	// missing names had neither a fresh answer nor a cached one.
	missing []string
}

// fqdnEntries returns the distinct FQDN entries in the table's address lists, in
// list order. Only the management rule can hold them; see Rule.acceptsFQDN.
func (t *Table) fqdnEntries() []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range t.Mgmt.CIDRs {
		if sanity.IsFQDNEntry(c) && !seen[c] {
			seen[c] = true
			out = append(out, c)
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

	// Folded back in list order, not completion order, so the warnings and the
	// error message name things in the order the operator wrote them.
	for i, name := range names {
		if answers[i].err == nil && len(answers[i].addrs) > 0 {
			res.byName[name] = elementsFor(answers[i].addrs)
			res.fresh = true
			cache[name] = dnsCacheEntry{IPs: res.byName[name], ResolvedAt: time.Now().UTC()}
			continue
		}
		if entry, ok := cache[name]; ok && len(entry.IPs) > 0 {
			res.byName[name] = entry.IPs
			res.stale = append(res.stale, name)
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
	// Persist on a fresh answer OR a prune: a prune with the resolver down must
	// still reach disk, or a name removed from the config during an outage
	// lingers in the cache until the resolver happens to succeed again.
	if res.fresh || pruned {
		cache.save(m.dnsCachePath)
	}

	if len(res.stale) > 0 {
		logx.As().Warn().Strs("fqdns", res.stale).Msg(
			"could not resolve these management allowlist entries; keeping their last-known addresses")
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

// expandFQDNs returns a copy of the table with every FQDN replaced by its
// resolved addresses, leaving the receiver untouched.
//
// A copy, not an in-place rewrite, because the two outputs of an apply need
// different views of the same table: the YAML records what the operator wrote
// and must keep the name, while the nft document must carry only literals. Doing
// this by construction rather than by a guard inside Render is what makes it
// impossible to emit a document containing a bare name — which matters, because
// nft would then resolve it itself at load time, silently bypassing this
// resolver, the cache fallback, and the never-empty rule below.
func (t *Table) expandFQDNs(byName map[string][]string) (*Table, error) {
	if len(byName) == 0 {
		return t, nil
	}

	out := *t
	cidrs := make([]string, 0, len(t.Mgmt.CIDRs))
	for _, c := range t.Mgmt.CIDRs {
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
	out.Mgmt.CIDRs = sortedDedupe(cidrs)
	return &out, nil
}

// checkResolvedMgmt refuses to render a management allowlist that the operator
// populated but resolution emptied.
//
// Distinct from checkMgmtLockout, which compares a mutation's before and after:
// this catches an allowlist that is entirely domain names when none of them
// resolve and none has ever been cached. The rendered rule would be
// `saddr @mgmt_addrs ... accept` against an empty set, which matches no packet
// and, under the input chain's default drop, takes every new SSH connection with
// it. There is no --force past this: an empty allowlist is a lock-out however it
// was arrived at.
func checkResolvedMgmt(t, resolved *Table, missing []string) error {
	if len(t.Mgmt.CIDRs) == 0 || len(resolved.Mgmt.CIDRs) > 0 {
		return nil
	}
	return errx.Decorate(
		errorx.IllegalState.New(
			"the management allowlist holds only domain names and none of them resolved (%v), so @mgmt_addrs would "+
				"render empty and this host would drop every new SSH connection. Nothing was changed", missing),
		reasons.PreconditionNotMet,
		"Check name resolution on this host: `getent hosts "+firstOr(missing, "<name>")+"`",
		"Add a literal address alongside the names: `solo-provisioner network firewall add --name mgmt --cidr <cidr>`")
}

// firstOr returns the first element of s, or fallback when s is empty, so an
// error hint can quote a real name instead of a placeholder.
func firstOr(s []string, fallback string) string {
	if len(s) > 0 {
		return s[0]
	}
	return fallback
}
