// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeResolver answers from a fixed table. A name absent from answers fails, so
// a test can model "this one host is unreachable" without affecting the others.
//
// Guarded by a mutex because resolveFQDNs looks names up concurrently; without
// it both the answers read and the call counter race under -race.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]string
	// calls counts lookups, so a test can assert the resolver was consulted (or
	// not) rather than inferring it from the rendered output.
	calls int
	// delay, when set, is slept per lookup to keep the lookups overlapping long enough
	// for concurrency to be observable in tests.
	delay time.Duration
	// inFlight/peakInFlight record concurrency directly, so a test can assert
	// the pass is concurrent without timing it against a shared CI runner.
	inFlight     int
	peakInFlight int
}

// setAnswers replaces the table between applies, under the same lock the
// lookups take.
func (f *fakeResolver) setAnswers(a map[string][]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = a
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeResolver) LookupIPv4(_ context.Context, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	f.calls++
	f.inFlight++
	if f.inFlight > f.peakInFlight {
		f.peakInFlight = f.inFlight
	}
	ips, ok := f.answers[host]
	delay := f.delay
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()

	if !ok {
		return nil, errors.New("no such host")
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		out = append(out, netip.MustParseAddr(ip))
	}
	return out, nil
}

// newDNSTestManager is newTestManager with a resolver wired in and the systemd
// timer stubbed, returning the manager, the nft path, and the timer's last
// requested state.
func newDNSTestManager(t *testing.T, r *fakeRunner, res Resolver, applyCount *int) (*Manager, string, *[]bool) {
	t.Helper()
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-weaver-host-firewall.nft")
	var timerWants []bool
	m := NewManagerWithConfig(Config{
		Runner:     r,
		Resolver:   res,
		NftPath:    nftPath,
		ConfigPath: filepath.Join(dir, "network-weaver-host-firewall.yaml"),
		LockPath:   filepath.Join(dir, ".applying"),
		ApplyViaService: func(context.Context) error {
			*applyCount++
			r.exists = true
			return nil
		},
		SyncRefreshTimer: func(_ context.Context, wanted bool) error {
			timerWants = append(timerWants, wanted)
			return nil
		},
	})
	return m, nftPath, &timerWants
}

// fqdnTable is a management allowlist mixing a literal with two names.
func fqdnTable() *Table {
	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"10.0.0.0/8", "jump.corp.example.com", "mon.corp.example.com"}
	tbl.InCluster.CIDRs = []string{"10.4.0.0/24"}
	return tbl
}

func TestFQDN_RenderedSetHoldsResolvedAddresses(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{
		"jump.corp.example.com": {"192.0.2.7"},
		"mon.corp.example.com":  {"198.51.100.4", "198.51.100.5"},
	}}
	applies := 0
	m, nftPath, timerWants := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), fqdnTable()))

	nft := readNft(t, nftPath)
	require.Contains(t, nft, "set mgmt_addrs")
	for _, want := range []string{"10.0.0.0/8", "192.0.2.7/32", "198.51.100.4/32", "198.51.100.5/32"} {
		require.Contains(t, nft, want, "resolved address must reach the nft set")
	}
	// The whole point of expand-then-render: a bare name in the document would be
	// resolved by nft itself at load time, bypassing this resolver entirely.
	require.NotContains(t, nft, "jump.corp.example.com")
	require.NotContains(t, nft, "mon.corp.example.com")

	// The persisted intent keeps the names.
	cfg := readFile(t, m.configPath)
	require.Contains(t, cfg, "jump.corp.example.com")
	require.NotContains(t, cfg, "192.0.2.7")

	require.Equal(t, []bool{true}, *timerWants, "a table holding names must get the refresh timer")
}

func TestFQDN_RecordOrderDoesNotChurnTheRender(t *testing.T) {
	rendered := make([]string, 0, 2)
	for _, order := range [][]string{
		{"198.51.100.5", "198.51.100.4", "192.0.2.7"},
		{"192.0.2.7", "198.51.100.5", "198.51.100.4"},
	} {
		r := &fakeRunner{}
		res := &fakeResolver{answers: map[string][]string{"jump.corp.example.com": order}}
		applies := 0
		m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

		tbl := NewTable()
		tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
		require.NoError(t, m.Apply(context.Background(), tbl))
		rendered = append(rendered, readNft(t, nftPath))
	}

	// DNS rotates record order between answers. Unsorted, every refresh would
	// look like a membership change and re-render the table for nothing.
	require.Equal(t, rendered[0], rendered[1],
		"a reordered answer must render byte-identically")
}

func TestFQDN_UnresolvableIsRejectedWhenOperatorSupplied(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"10.0.0.0/8", "typo.corp.example.com"}
	err := m.Apply(context.Background(), tbl)

	// Failing loudly beats installing a firewall that silently omits a host the
	// operator asked for.
	require.Error(t, err)
	require.Contains(t, err.Error(), "typo.corp.example.com")
	require.Equal(t, 0, applies, "nothing may be applied when a supplied name cannot be resolved")
}

func TestFQDN_AllNamesUnresolvableIsRefusedEvenOnRefresh(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"jump.corp.example.com": {"192.0.2.7"}}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))

	// The name stops resolving and its cache entry is lost, so nothing is left to
	// render. An empty @mgmt_addrs under the default-drop input chain is a
	// lock-out, so even the tolerant refresh path must refuse it.
	res.setAnswers(map[string][]string{})
	require.NoError(t, os.Remove(m.dnsCachePath))

	err := m.RefreshDNS(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "would render empty")
	require.Equal(t, 1, applies, "the refusal must happen before anything is applied")
}

func TestFQDN_PartialFailureKeepsEachNameSeparate(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{
		"jump.corp.example.com": {"192.0.2.7"},
		"mon.corp.example.com":  {"198.51.100.4"},
	}}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), fqdnTable()))

	// One name rotates, the other stops resolving entirely. This is the case a
	// merged set cannot represent: without per-name attribution, keeping the
	// failed name's address means pinning the rotated name's old one too.
	res.setAnswers(map[string][]string{"jump.corp.example.com": {"192.0.2.99"}})
	require.NoError(t, m.RefreshDNS(context.Background()))

	nft := readNft(t, nftPath)
	require.Contains(t, nft, "192.0.2.99/32", "the rotated name must follow its new address")
	require.NotContains(t, nft, "192.0.2.7/32", "the rotated name's old address must go")
	require.Contains(t, nft, "198.51.100.4/32", "the unresolvable name must keep its last-known address")
	require.Equal(t, 2, applies)
}

func TestFQDN_RefreshIsANoOpWhenAddressesAreUnchanged(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"jump.corp.example.com": {"192.0.2.7"}}}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))
	nftBefore := readNft(t, nftPath)
	require.NoFileExists(t, prevPath(m), "the first apply retains no previous generation")

	require.NoError(t, m.RefreshDNS(context.Background()))

	// It runs every few minutes. Reloading an identical ruleset would also
	// re-trigger the shared oneshot's workload-policy replay, 288 times a day.
	require.Equal(t, 1, applies, "an unchanged resolution must not reload the kernel")
	require.Equal(t, nftBefore, readNft(t, nftPath))
	require.NoFileExists(t, prevPath(m), "a skipped refresh must not burn the retained generation")
	require.Greater(t, res.callCount(), 1, "the resolver must still be consulted")
}

func TestFQDN_CacheSurvivesAndIsPruned(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{
		"jump.corp.example.com": {"192.0.2.7"},
		"mon.corp.example.com":  {"198.51.100.4"},
	}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), fqdnTable()))

	// A fresh map per read: json.Unmarshal merges into a non-nil map rather than
	// replacing it, which would hide the pruning this test is about.
	readCache := func() dnsCache {
		t.Helper()
		var c dnsCache
		require.NoError(t, json.Unmarshal([]byte(readFile(t, m.dnsCachePath)), &c))
		return c
	}

	cache := readCache()
	require.Len(t, cache, 2)
	require.Equal(t, []string{"192.0.2.7/32"}, cache["jump.corp.example.com"].IPs)
	require.False(t, cache["jump.corp.example.com"].ResolvedAt.IsZero())

	// Dropping a name from the allowlist must drop it from the cache, or the file
	// accumulates hosts this firewall no longer mentions.
	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))

	cache = readCache()
	require.Len(t, cache, 1)
	require.NotContains(t, cache, "mon.corp.example.com")
}

func TestFQDN_CorruptCacheDegradesRatherThanFailing(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"jump.corp.example.com": {"192.0.2.7"}}}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))
	require.NoError(t, os.WriteFile(m.dnsCachePath, []byte("{ not json"), 0o600))

	// The cache is a fallback, never the authority: an unreadable one costs the
	// last-known addresses and nothing else.
	require.NoError(t, m.RefreshDNS(context.Background()))
	require.Contains(t, readNft(t, nftPath), "192.0.2.7/32")
}

func TestFQDN_DeleteRemovesTheCacheAndTimer(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"jump.corp.example.com": {"192.0.2.7"}}}
	applies := 0
	m, _, timerWants := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))
	require.FileExists(t, m.dnsCachePath)

	require.NoError(t, m.Delete(context.Background()))

	require.NoFileExists(t, m.dnsCachePath,
		"last-known addresses for a table that no longer exists would be inherited by the next create")
	require.Equal(t, []bool{true, false}, *timerWants, "delete must tear the timer down")
}

func TestFQDN_LiteralOnlyTableGetsNoTimerAndNoResolver(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{}}
	applies := 0
	m, _, timerWants := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), sampleTable()))

	require.Equal(t, 0, res.callCount(), "a literal-only allowlist must not touch the resolver")
	require.Equal(t, []bool{false}, *timerWants, "and must carry no refresh timer")
	require.NoFileExists(t, m.dnsCachePath)
}

func TestFQDN_NormalisationCollapsesCaseAndTrailingDot(t *testing.T) {
	tbl := NewTable()
	require.NoError(t, tbl.Mgmt.SetCIDRs([]string{
		"Jump.Corp.Example.COM.", "jump.corp.example.com", "JUMP.corp.example.com.",
	}))

	require.Equal(t, []string{"jump.corp.example.com"}, tbl.Mgmt.CIDRs,
		"one host must occupy one slot regardless of how it was spelled")

	// And removal must match across spellings, or `remove` silently no-ops.
	tbl.Mgmt.RemoveCIDRs([]string{"JUMP.Corp.Example.com."})
	require.Empty(t, tbl.Mgmt.CIDRs)
}

func TestFQDN_OnlyInClusterRejectsNames(t *testing.T) {
	tbl := NewTable()

	// Every list an operator maintains by hand takes names.
	require.NoError(t, tbl.Mgmt.AddCIDRs([]string{"jump.corp.example.com"}))
	require.NoError(t, tbl.Blocked.AddCIDRs([]string{"bad.corp.example.com"}))

	// The pod CIDR is auto-detected from the node rather than typed, so there is
	// no name for it to accept.
	require.Error(t, tbl.InCluster.AddCIDRs([]string{"pods.corp.example.com"}))
}

func TestFQDN_AllowRuleAcceptsNames(t *testing.T) {
	// An operator-declared allow rule is the same shape as mgmt — a source list x
	// port list x proto accept — and shares the same reason to accept a name: an
	// operator maintaining it by hand, for a host they often know only by name.
	allow := Rule{Name: "monitoring"}
	require.NoError(t, allow.AddCIDRs([]string{"probe.corp.example.com"}))
	require.Equal(t, []string{"probe.corp.example.com"}, allow.CIDRs)
}

func TestFQDN_AllowRuleNameResolvesThroughApply(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"probe.corp.example.com": {"198.51.100.9"}}}
	applies := 0
	m, nftPath, timerWants := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), sampleTable()))
	_, err := m.CreateRule(context.Background(), Rule{Name: "monitoring", Proto: ProtoTCP}, false)
	require.NoError(t, err)
	require.NoError(t, m.Add(context.Background(), "monitoring", []string{"probe.corp.example.com"}, []string{"9100"}))

	nft := readNft(t, nftPath)
	require.Contains(t, nft, "198.51.100.9/32", "the allow rule's name must resolve into its own set")
	require.NotContains(t, nft, "probe.corp.example.com")

	cfg := readFile(t, m.configPath)
	require.Contains(t, cfg, "probe.corp.example.com", "the persisted intent keeps the name")

	require.Equal(t, []bool{false, false, true}, *timerWants,
		"sampleTable and CreateRule hold no names yet; Add introduces the first one")
}

func TestFQDN_AllowRuleUnresolvedIsWarnedNotRefused(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"probe.corp.example.com": {"198.51.100.9"}}}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), sampleTable()))
	_, err := m.CreateRule(context.Background(), Rule{Name: "monitoring", Proto: ProtoTCP}, false)
	require.NoError(t, err)
	require.NoError(t, m.Add(context.Background(), "monitoring", []string{"probe.corp.example.com"}, []string{"9100"}))

	// The name stops resolving and its cache entry is lost. Unlike mgmt, an allow
	// rule resolving to nothing is not a hard refusal — even on the tolerant
	// refresh path, it just renders as incomplete, same as any other allow rule
	// that has not been fully populated yet.
	res.setAnswers(map[string][]string{})
	require.NoError(t, os.Remove(m.dnsCachePath))
	require.NoError(t, m.RefreshDNS(context.Background()))

	// The set declarations always render (empty), but the accept rule that would
	// admit traffic through them is gated on non-empty CIDRs and does not.
	nft := readNft(t, nftPath)
	require.NotContains(t, nft, "@monitoring tcp dport @monitoring_ports accept",
		"an allow rule with no resolved addresses admits nothing")

	cfg := readFile(t, m.configPath)
	require.Contains(t, cfg, "probe.corp.example.com", "the persisted intent still keeps the name")
}

func TestFQDN_MgmtStillHardRefusesWhileAllowRuleOnlyWarns(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{
		"jump.corp.example.com":  {"192.0.2.7"},
		"probe.corp.example.com": {"198.51.100.9"},
	}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	require.NoError(t, tbl.UpsertAllow(Rule{Name: "monitoring", CIDRs: []string{"probe.corp.example.com"}, Ports: []string{"9100"}}))
	require.NoError(t, m.Apply(context.Background(), tbl))

	// Both names stop resolving and their cache entries are lost. mgmt resolving
	// to nothing refuses the whole refresh; an allow rule resolving to nothing on
	// its own would not have — mustResolveToSomething is what draws that line,
	// and mgmt hitting it first (before the Allow loop runs) is what this pins.
	res.setAnswers(map[string][]string{})
	require.NoError(t, os.Remove(m.dnsCachePath))

	err := m.RefreshDNS(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "\"mgmt\"")
}

func TestFQDN_EnumerationFollowsRenderOrder(t *testing.T) {
	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"jump.corp.example.com"}
	tbl.Blocked.CIDRs = []string{"bad.corp.example.com"}
	require.NoError(t, tbl.UpsertAllow(Rule{Name: "zzz-rule", CIDRs: []string{"z.corp.example.com"}, Ports: []string{"1"}}))
	require.NoError(t, tbl.UpsertAllow(Rule{Name: "aaa-rule", CIDRs: []string{"a.corp.example.com"}, Ports: []string{"1"}}))

	require.Equal(t,
		[]string{"jump.corp.example.com", "bad.corp.example.com", "a.corp.example.com", "z.corp.example.com"},
		tbl.fqdnEntries(),
		"Table.rules order: mgmt, blocked, in_cluster, then allow rules by name")
}

func TestFQDN_MasklessIPStillAsksForAPrefixLength(t *testing.T) {
	tbl := NewTable()
	err := tbl.Mgmt.AddCIDRs([]string{"10.0.0.1"})

	// A bare address must not be mistaken for a hostname and handed to the
	// resolver; the operator wants the "you forgot the mask" error.
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "cidr")
}

func TestSplitCIDRs_RefusesAnUnexpandedName(t *testing.T) {
	// Defence in depth behind expand-then-render. Skipping the entry instead
	// would render `set mgmt_addrs { ... }` with no elements clause at all — a
	// document nft accepts, and which drops every new SSH connection.
	_, _, err := splitCIDRs([]string{"10.0.0.0/8", "jump.corp.example.com"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "jump.corp.example.com")
}

func TestFQDN_ConfigRoundTripKeepsTheName(t *testing.T) {
	tbl := fqdnTable()
	require.NoError(t, tbl.Validate())

	data, err := FileConfigFromTable(tbl).Marshal()
	require.NoError(t, err)
	require.Contains(t, string(data), "jump.corp.example.com")

	cfg, err := ParseConfig(data)
	require.NoError(t, err)
	back, err := cfg.Table()
	require.NoError(t, err)

	// `show --output yaml | create --from-file` must be a no-op, names included.
	require.Equal(t, tbl.Mgmt.CIDRs, back.Mgmt.CIDRs)
}

func TestFQDN_FromFileAcceptsNamesOutsideInCluster(t *testing.T) {
	// The reserved rule names are assigned by NewTable before Validate runs, so
	// the per-rule dispatch is live on the --from-file path too.
	withEntries := func(mgmt, blocked, inCluster string) []byte {
		return []byte("version: 1\n" +
			"mgmt:\n  cidrs:\n    - " + mgmt + "\n  ports:\n    - \"22\"\n" +
			"blocked:\n  cidrs:\n    - " + blocked + "\n" +
			"in_cluster:\n  cidrs:\n    - " + inCluster + "\n")
	}

	cfg, err := ParseConfig(withEntries("jump.corp.example.com", "bad.corp.example.com", "10.4.0.0/14"))
	require.NoError(t, err)
	tbl, err := cfg.Table()
	require.NoError(t, err)
	require.Equal(t, []string{"jump.corp.example.com"}, tbl.Mgmt.CIDRs)
	require.Equal(t, []string{"bad.corp.example.com"}, tbl.Blocked.CIDRs)

	_, err = ParseConfig(withEntries("10.0.0.0/8", "203.0.113.0/24", "pods.corp.example.com"))
	require.Error(t, err, "a name in the pod CIDR must be rejected on the file path too")
	require.Contains(t, err.Error(), "pods.corp.example.com")
}

func TestFQDN_NamesAreResolvedConcurrently(t *testing.T) {
	const perLookup = 150 * time.Millisecond

	r := &fakeRunner{}
	res := &fakeResolver{
		delay: perLookup,
		answers: map[string][]string{
			"a.corp.example.com": {"192.0.2.1"},
			"b.corp.example.com": {"192.0.2.2"},
			"c.corp.example.com": {"192.0.2.3"},
			"d.corp.example.com": {"192.0.2.4"},
		},
	}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{
		"a.corp.example.com", "b.corp.example.com", "c.corp.example.com", "d.corp.example.com",
	}

	require.NoError(t, m.Apply(context.Background(), tbl))

	// resolveTimeout budgets the whole pass, not each name. A sequential pass
	// would fail names that are perfectly reachable, so assert the peak
	// concurrency directly rather than inferring it from elapsed wall time
	// (flaky on a loaded, shared CI runner).
	require.Equal(t, len(tbl.Mgmt.CIDRs), res.peakInFlight,
		"every name must be looked up at once")

	nft := readNft(t, nftPath)
	for _, want := range []string{"192.0.2.1/32", "192.0.2.2/32", "192.0.2.3/32", "192.0.2.4/32"} {
		require.Contains(t, nft, want)
	}
}

func TestFQDN_WarningsFollowListOrderNotCompletionOrder(t *testing.T) {
	r := &fakeRunner{}
	// None resolve, so all four land in `missing` — which is what the operator
	// error quotes. Concurrent lookups finish in arbitrary order, so the fold-back
	// has to re-impose the order the operator wrote.
	res := &fakeResolver{answers: map[string][]string{}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"10.0.0.0/8", "z.corp.example.com", "a.corp.example.com", "m.corp.example.com"}

	for i := 0; i < 20; i++ {
		err := m.Apply(context.Background(), tbl)
		require.Error(t, err)
		require.Contains(t, err.Error(), "[z.corp.example.com a.corp.example.com m.corp.example.com]",
			"unresolved names must be reported in the order they were written")
	}
}

// TestFQDN_SurvivesUnrelatedMutations is the invariant the whole design rests on:
// the YAML is a statement of the operator's intent, written from the in-memory
// Table, and is never regenerated from the kernel. So a name put there once
// survives every later mutation of anything else in the table — there is no
// point at which it has to be "put back", because nothing takes it out.
//
// The one exception is Manager.load's fallback to Parse when the YAML is missing
// entirely; that tier recovers from the rendered .nft, which holds only resolved
// addresses. See Parse's doc comment.
func TestFQDN_SurvivesUnrelatedMutations(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"jump.corp.example.com": {"192.0.2.7"}}}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"10.0.0.0/8", "jump.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))

	ctx := context.Background()
	steps := []struct {
		what string
		do   func() error
	}{
		{"add a mgmt port", func() error { return m.Add(ctx, RuleMgmt, nil, []string{"2222"}) }},
		{"add a blocked CIDR", func() error { return m.Add(ctx, RuleBlocked, []string{"203.0.113.0/24"}, nil) }},
		{"declare an allow rule", func() error {
			_, err := m.CreateRule(ctx, Rule{Name: "monitoring", Proto: ProtoTCP}, false)
			return err
		}},
		{"populate the allow rule", func() error {
			return m.Add(ctx, "monitoring", []string{"198.51.100.0/24"}, []string{"9100"})
		}},
		{"reapply", func() error { return m.Reapply(ctx) }},
		{"refresh dns", func() error { return m.RefreshDNS(ctx) }},
	}

	for _, step := range steps {
		require.NoError(t, step.do(), step.what)

		// The name is still the persisted intent...
		cfg := readFile(t, m.configPath)
		require.Contains(t, cfg, "jump.corp.example.com",
			"the FQDN must survive: %s", step.what)
		require.NotContains(t, cfg, "192.0.2.7",
			"the persisted config must never acquire the resolved address: %s", step.what)

		// ...and the kernel artifact still carries only literals.
		nft := readNft(t, nftPath)
		require.Contains(t, nft, "192.0.2.7/32", step.what)
		require.NotContains(t, nft, "jump.corp.example.com", step.what)
	}

	// And a reload from disk still sees the name, not the address it resolved to.
	reloaded, err := m.Table(ctx)
	require.NoError(t, err)
	require.Contains(t, reloaded.Mgmt.CIDRs, "jump.corp.example.com")
	require.NotContains(t, reloaded.Mgmt.CIDRs, "192.0.2.7/32")
}

// blockedFQDNTable is a block list mixing a literal with a name, so a test can
// show that losing the name is refused even while the rule still renders
// something.
func blockedFQDNTable() *Table {
	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"10.0.0.0/8"}
	tbl.Blocked.CIDRs = []string{"203.0.113.0/24", "bad.corp.example.com"}
	tbl.InCluster.CIDRs = []string{"10.4.0.0/24"}
	return tbl
}

func TestFQDN_BlockedNameResolvesThroughApply(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"bad.corp.example.com": {"198.51.100.4"}}}
	applies := 0
	m, nftPath, timerWants := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), blockedFQDNTable()))

	nft := readNft(t, nftPath)
	require.Contains(t, nft, "198.51.100.4/32", "the resolved address must reach @blocked_addrs")
	require.Contains(t, nft, "203.0.113.0/24", "and the literal alongside it must survive")
	require.NotContains(t, nft, "bad.corp.example.com", "nft must never see the name itself")

	require.Contains(t, readFile(t, m.configPath), "bad.corp.example.com", "the config keeps the name")
	require.Equal(t, []bool{true}, *timerWants, "a name anywhere in the table installs the refresh timer")
}

func TestFQDN_BlockedUnresolvedIsRefusedOnTheTolerantPaths(t *testing.T) {
	// The core of #1099. Everywhere else an unresolved name withdraws access and
	// the tolerant paths let it through so a resolver outage cannot stop an
	// operator re-asserting their firewall. Here the same event *grants* access,
	// so there is no path on which a warning is enough.
	for _, tc := range []struct {
		name string
		run  func(*Manager) error
	}{
		{name: "refresh-dns", run: func(m *Manager) error { return m.RefreshDNS(context.Background()) }},
		{name: "reapply", run: func(m *Manager) error { return m.Reapply(context.Background()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRunner{}
			res := &fakeResolver{answers: map[string][]string{"bad.corp.example.com": {"198.51.100.4"}}}
			applies := 0
			m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

			require.NoError(t, m.Apply(context.Background(), blockedFQDNTable()))
			nftBefore := readNft(t, nftPath)

			// Never-resolved, not merely stale: the answer is gone and so is the
			// cache that would have covered for it.
			res.setAnswers(map[string][]string{})
			require.NoError(t, os.Remove(m.dnsCachePath))

			err := tc.run(m)
			require.Error(t, err)
			require.Contains(t, err.Error(), "bad.corp.example.com")
			require.Contains(t, err.Error(), "\"blocked\"")

			// Refusing writes nothing, which is what keeps the host denying the
			// address it was already denying.
			require.Equal(t, 1, applies)
			require.Equal(t, nftBefore, readNft(t, nftPath))
		})
	}
}

func TestFQDN_BlockedRefusesWhileTheRuleStillRendersSomething(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"bad.corp.example.com": {"198.51.100.4"}}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), blockedFQDNTable()))
	res.setAnswers(map[string][]string{})
	require.NoError(t, os.Remove(m.dnsCachePath))

	// checkResolvedRule waits for a rule to render empty; that is the wrong
	// threshold for a block list, where the literal alongside the name keeps the
	// rule looking healthy while the host the name pointed at is reachable again.
	err := m.RefreshDNS(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad.corp.example.com")
	require.Equal(t, 1, applies)
}

func TestFQDN_BlockedKeepsCachedAddressesThroughAResolverOutage(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"bad.corp.example.com": {"198.51.100.4"}}}
	applies := 0
	m, nftPath, _ := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), blockedFQDNTable()))

	// The cache is what makes the refusal above affordable: a name that has
	// resolved even once rides out an outage as *stale* rather than *missing*,
	// so the timer does not fail on a blip.
	res.setAnswers(map[string][]string{})
	require.NoError(t, m.RefreshDNS(context.Background()))

	require.Contains(t, readNft(t, nftPath), "198.51.100.4/32",
		"a cached block-list entry must keep denying its last-known address")
}

func TestFQDN_BlockedUnresolvedAlsoRefusesUnrelatedMutations(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"bad.corp.example.com": {"198.51.100.4"}}}
	applies := 0
	m, _, _ := newDNSTestManager(t, r, res, &applies)

	require.NoError(t, m.Apply(context.Background(), blockedFQDNTable()))
	res.setAnswers(map[string][]string{})
	require.NoError(t, os.Remove(m.dnsCachePath))

	// Resolution covers the whole table, so an unresolvable block-list entry
	// stands in the way of edits that have nothing to do with it. The error has
	// to name the entry, or the operator has no way to see why.
	err := m.Add(context.Background(), RuleMgmt, []string{"192.168.0.0/16"}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad.corp.example.com")

	// And removing the offending entry is the documented way out.
	require.NoError(t, m.Remove(context.Background(), RuleBlocked, []string{"bad.corp.example.com"}, nil, false))
}

func TestFQDN_TimerFollowsBlockListNamesToo(t *testing.T) {
	r := &fakeRunner{}
	res := &fakeResolver{answers: map[string][]string{"bad.corp.example.com": {"198.51.100.4"}}}
	applies := 0
	m, _, timerWants := newDNSTestManager(t, r, res, &applies)

	tbl := NewTable()
	tbl.Mgmt.CIDRs = []string{"10.0.0.0/8"}
	tbl.Blocked.CIDRs = []string{"bad.corp.example.com"}
	require.NoError(t, m.Apply(context.Background(), tbl))
	require.Equal(t, []bool{true}, *timerWants, "the last name may live in the block list")

	require.NoError(t, m.Set(context.Background(), RuleBlocked, []string{"203.0.113.0/24"}, nil, false))
	require.Equal(t, []bool{true, false}, *timerWants, "and removing it must take the timer with it")
}
