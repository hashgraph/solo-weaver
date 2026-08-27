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
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeResolver answers from a fixed table. A name absent from answers fails, so
// a test can model "this one host is unreachable" without affecting the others.
type fakeResolver struct {
	answers map[string][]string
	// calls counts lookups, so a test can assert the resolver was consulted (or
	// not) rather than inferring it from the rendered output.
	calls int
}

func (f *fakeResolver) LookupIPv4(_ context.Context, host string) ([]netip.Addr, error) {
	f.calls++
	ips, ok := f.answers[host]
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
	res.answers = map[string][]string{}
	require.NoError(t, os.Remove(m.dnsCachePath))

	err := m.RefreshDNS(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "drop every new SSH connection")
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
	res.answers = map[string][]string{"jump.corp.example.com": {"192.0.2.99"}}
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
	require.Greater(t, res.calls, 1, "the resolver must still be consulted")
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

	require.Equal(t, 0, res.calls, "a literal-only allowlist must not touch the resolver")
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

func TestFQDN_OnlyTheMgmtRuleAcceptsNames(t *testing.T) {
	tbl := NewTable()

	require.NoError(t, tbl.Mgmt.AddCIDRs([]string{"jump.corp.example.com"}))

	// A resolver outage must never be able to change what the block list drops or
	// which pods reach host services.
	require.Error(t, tbl.Blocked.AddCIDRs([]string{"bad.corp.example.com"}))
	require.Error(t, tbl.InCluster.AddCIDRs([]string{"pods.corp.example.com"}))

	allow := Rule{Name: "monitoring"}
	require.Error(t, allow.AddCIDRs([]string{"probe.corp.example.com"}))
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

func TestFQDN_FromFileRejectsNamesOutsideMgmt(t *testing.T) {
	// The reserved rule names are assigned by NewTable before Validate runs, so
	// the mgmt-only dispatch is live on the --from-file path too.
	withMgmt := func(mgmt, blocked string) []byte {
		return []byte("version: 1\n" +
			"mgmt:\n  cidrs:\n    - " + mgmt + "\n  ports:\n    - \"22\"\n" +
			"blocked:\n  cidrs:\n    - " + blocked + "\n" +
			"in_cluster:\n  cidrs: []\n")
	}

	cfg, err := ParseConfig(withMgmt("jump.corp.example.com", "203.0.113.0/24"))
	require.NoError(t, err)
	tbl, err := cfg.Table()
	require.NoError(t, err)
	require.Equal(t, []string{"jump.corp.example.com"}, tbl.Mgmt.CIDRs)

	_, err = ParseConfig(withMgmt("10.0.0.0/8", "bad.corp.example.com"))
	require.Error(t, err, "a name in the block list must be rejected on the file path too")
	require.Contains(t, err.Error(), "bad.corp.example.com")
}
