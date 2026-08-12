// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// update regenerates the golden .nft fixtures instead of comparing against
// them: `go test ./internal/network/firewall/... -update`. Review the diff
// before committing a regenerated golden.
var update = flag.Bool("update", false, "regenerate golden testdata files")

// fakeRunner is an in-memory Runner for tests: it tracks table existence
// without touching the kernel. Apply is intentionally absent — live rule
// application goes through applyViaService, not the Runner.
type fakeRunner struct {
	exists  bool
	listOut string
	deleted bool
}

func (f *fakeRunner) List(_ context.Context) (string, error) { return f.listOut, nil }
func (f *fakeRunner) Delete(_ context.Context) error         { f.deleted = true; f.exists = false; return nil }
func (f *fakeRunner) Exists(_ context.Context) (bool, error) { return f.exists, nil }

func sampleTable() *Table {
	return &Table{
		MgmtCIDRs:      []string{"10.0.0.0/8", "192.168.0.0/16"},
		BlockedCIDRs:   []string{"203.0.113.0/24"},
		InClusterPorts: []int{4244, 6443, 7472, 10250},
		SSHPort:        22,
		PodCIDR:        "10.4.0.0/24",
	}
}

// dualStackTable is sampleTable with IPv6 members mixed into every dimension,
// exercising the ipv6_addr sets, the `ip6` rules, and the v6 in-cluster rule.
func dualStackTable() *Table {
	return &Table{
		MgmtCIDRs:      []string{"10.0.0.0/8", "2001:db8:a11::/48"},
		BlockedCIDRs:   []string{"203.0.113.0/24", "2001:db8:bad::/48"},
		InClusterPorts: []int{4244, 6443, 7472, 10250},
		SSHPort:        22,
		PodCIDR:        "10.4.0.0/24",
		PodCIDR6:       "2001:db8:c0de::/64",
	}
}

// chainBody returns the rules of the named chain from a rendered document,
// excluding the `chain <name> {` header and the closing brace. Ordering
// assertions have to be made per-chain: once the family dispatch is in place
// the document order is no longer the evaluation order, because the jumped-to
// chains are defined below the base chain but run before it reaches the rules
// that follow the dispatch.
//
// The header is excluded so that negative assertions stay meaningful — the
// chain names themselves carry family tokens, so leaving `chain input_ipv6 {`
// in the returned text would make a NotContains check for "ipv6" match the
// header instead of a rule.
func chainBody(t *testing.T, doc, name string) string {
	t.Helper()
	header := "\tchain " + name + " {\n"
	start := strings.Index(doc, header)
	require.Greater(t, start, -1, "chain %s not found in rendered document", name)
	rest := doc[start+len(header):]
	end := strings.Index(rest, "\n\t}\n")
	require.Greater(t, end, -1, "chain %s is not terminated", name)
	return rest[:end]
}

// newTestManager wires a Manager with a fakeRunner and temp paths. The
// applyViaService closure sets r.exists = true and increments applyCount so
// tests can assert on how many times rules were applied without touching
// systemd or the kernel.
func newTestManager(t *testing.T, r *fakeRunner, applyCount *int) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-weaver-host-firewall.nft")
	m := NewManagerWithConfig(Config{
		Runner:   r,
		NftPath:  nftPath,
		LockPath: filepath.Join(dir, ".applying"),
		ApplyViaService: func(context.Context) error {
			*applyCount++
			r.exists = true
			return nil
		},
	})
	return m, nftPath
}

func TestRender_SecurityInvariants(t *testing.T) {
	doc, err := sampleTable().Render()
	require.NoError(t, err)

	// The chain must default-drop and must always admit SSH from the mgmt
	// allowlist — a default-drop without an SSH allow would lock the host out.
	require.Contains(t, doc, "policy drop;")
	require.Contains(t, doc, "ip saddr @mgmt_addrs tcp dport 22 accept")
	require.Contains(t, doc, "elements = { 10.0.0.0/8, 192.168.0.0/16 }")
	require.Contains(t, doc, "elements = { 4244, 6443, 7472, 10250 }")
	// The operator block list is a distinct set from the mgmt allowlist and
	// must be dropped before anything else, including established/related.
	require.Contains(t, doc, "set blocked_addrs { type ipv4_addr; flags interval; elements = { 203.0.113.0/24 }; }")
	require.Contains(t, doc, "ip saddr @blocked_addrs drop")
	// ICMP is a static, safe ruleset: full ICMP from mgmt, and from everyone
	// else the path-health subset (PMTUD + traceroute) plus rate-limited echo.
	require.Contains(t, doc, "ip saddr @mgmt_addrs icmp type { echo-request, echo-reply, destination-unreachable, time-exceeded, parameter-problem } accept")
	// Over-budget echo is discarded first, so a single accept can cover every
	// admitted type. The meter stays scoped to echo-request: metering the whole
	// set would share one bucket and let a ping flood starve the error signals.
	require.Contains(t, doc, "icmp type echo-request limit rate over 10/second drop")
	require.Contains(t, doc, "icmp type { destination-unreachable, time-exceeded, echo-request } accept")
	require.Contains(t, doc, "ip saddr 10.4.0.0/24 tcp dport @in_cluster_ports accept")

	// Ordering is load-bearing. ICMP must be evaluated BEFORE the conntrack
	// fast-path: netfilter conntrack tracks ICMP echo flows, so if the
	// established fast-path ran first, every packet of a sustained ping after
	// the first would bypass the rate limit. The l4proto dispatch is what puts
	// ICMP first, so it must precede the ct vmap in the base chain.
	base := chainBody(t, doc, "input")
	icmpDispatchIdx := strings.Index(base, "meta l4proto vmap { icmp : jump input_icmp_ipv4, icmpv6 : jump input_icmp_ipv6 }")
	ctIdx := strings.Index(base, "ct state vmap { established : accept, related : accept, invalid : drop }")
	require.Greater(t, icmpDispatchIdx, -1, "base chain must dispatch ICMP by l4proto")
	require.Greater(t, ctIdx, -1, "base chain must carry the collapsed conntrack vmap")
	require.Less(t, icmpDispatchIdx, ctIdx, "ICMP dispatch must precede the conntrack fast-path")

	// Within the ICMP chain: invalid is dropped first, so a forged ICMP error
	// cannot be admitted by the blanket path-health accept that follows; then the
	// over-budget drop, which must precede that accept or the meter never binds.
	icmp4 := chainBody(t, doc, "input_icmp_ipv4")
	invalidIdx := strings.Index(icmp4, "ct state invalid drop")
	limitIdx := strings.Index(icmp4, "icmp type echo-request limit rate over 10/second drop")
	acceptIdx := strings.Index(icmp4, "icmp type { destination-unreachable, time-exceeded, echo-request } accept")
	require.Greater(t, invalidIdx, -1, "ICMPv4 chain must drop invalid before the path-health accept")
	require.Greater(t, limitIdx, -1, "ICMPv4 chain must meter echo-request")
	require.Less(t, invalidIdx, limitIdx, "invalid drop must precede the ICMP rules")
	require.Less(t, limitIdx, acceptIdx, "the over-budget drop must precede the accept it guards")

	// The block list must be evaluated before the conntrack fast-path so an
	// operator-added CIDR also kills already-open connections, not just new ones.
	blockedIdx := strings.Index(base, "ip saddr @blocked_addrs drop")
	require.Greater(t, blockedIdx, -1)
	require.Less(t, blockedIdx, ctIdx, "blocked-CIDR drop must precede the conntrack fast-path")
}

// TestRender_BlockListReachesEveryPath pins the block list's scope. A CIDR in
// @blocked_addrs means "blocked on this node": dropped ahead of conntrack on the
// way in (which also covers pod-bound forwarded traffic), and dropped as a
// destination on the way out. Inbound-only is not enough — blocking a peer does
// not stop this host from dialing it, and the replies to a host-initiated
// connection are admitted by the input chain's established accept.
func TestRender_BlockListReachesEveryPath(t *testing.T) {
	doc, err := dualStackTable().Render()
	require.NoError(t, err)

	// Priority must be below conntrack's -200, or the early drop buys nothing.
	pre := chainBody(t, doc, "prerouting_blocklist")
	require.Contains(t, pre, "type filter hook prerouting priority -300; policy accept;")
	require.Contains(t, pre, "ip saddr @blocked_addrs drop")
	require.Contains(t, pre, "ip6 saddr @blocked_addrs6 drop")

	// The output chain is block-list symmetry, NOT an egress allowlist: it must
	// stay `policy accept` and must never grow a rule that gates normal traffic.
	out := chainBody(t, doc, "output")
	require.Contains(t, out, "type filter hook output priority 0; policy accept;")
	require.Contains(t, out, "ip daddr @blocked_addrs drop")
	require.Contains(t, out, "ip6 daddr @blocked_addrs6 drop")
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "type filter") {
			continue
		}
		require.Contains(t, line, "@blocked_addrs", "output chain must carry block-list rules only, got %q", line)
	}

	// The input copy is redundant with prerouting for wire traffic but is kept
	// deliberately, so the block list's position relative to the conntrack
	// fast-path remains a property of the input chain itself.
	require.Contains(t, chainBody(t, doc, "input"), "ip saddr @blocked_addrs drop")
}

// TestRender_FamilySplit pins the point of the per-family chains: a packet must
// never be evaluated against a rule belonging to the other address family.
func TestRender_FamilySplit(t *testing.T) {
	doc, err := dualStackTable().Render()
	require.NoError(t, err)

	require.Contains(t, chainBody(t, doc, "input"),
		"meta nfproto vmap { ipv4 : jump input_ipv4, ipv6 : jump input_ipv6 }")

	v4 := chainBody(t, doc, "input_ipv4")
	require.Contains(t, v4, "ip saddr @mgmt_addrs tcp dport 22 accept")
	require.NotContains(t, v4, "ip6 ")

	v6 := chainBody(t, doc, "input_ipv6")
	require.Contains(t, v6, "ip6 saddr @mgmt_addrs6 tcp dport 22 accept")
	require.NotContains(t, v6, "ip saddr")

	icmp4 := chainBody(t, doc, "input_icmp_ipv4")
	require.Contains(t, icmp4, "icmp type echo-request limit rate over 10/second drop")
	require.NotContains(t, icmp4, "icmpv6")

	icmp6 := chainBody(t, doc, "input_icmp_ipv6")
	require.Contains(t, icmp6, "icmpv6 type echo-request limit rate over 10/second drop")
	require.NotContains(t, icmp6, "icmp type")
}

func TestRender_NoMgmtNoPod(t *testing.T) {
	tbl := NewTable() // no mgmt CIDRs, no pod CIDR
	doc, err := tbl.Render()
	require.NoError(t, err)

	// Empty mgmt set renders without an elements clause; no pod CIDR means no
	// in-cluster rule line. Both families' sets are always declared (dual-stack).
	require.Contains(t, doc, "set mgmt_addrs { type ipv4_addr; flags interval; }")
	require.Contains(t, doc, "set mgmt_addrs6 { type ipv6_addr; flags interval; }")
	require.Contains(t, doc, "set blocked_addrs { type ipv4_addr; flags interval; }")
	require.Contains(t, doc, "set blocked_addrs6 { type ipv6_addr; flags interval; }")
	require.NotContains(t, doc, "tcp dport @in_cluster_ports accept")
}

func TestRender_DualStack(t *testing.T) {
	doc, err := dualStackTable().Render()
	require.NoError(t, err)

	// Each family's members land in its own set; mixed --mgmt/--blocked lists
	// are split by family, not smuggled into the wrong-typed set.
	require.Contains(t, doc, "set mgmt_addrs { type ipv4_addr; flags interval; elements = { 10.0.0.0/8 }; }")
	require.Contains(t, doc, "set mgmt_addrs6 { type ipv6_addr; flags interval; elements = { 2001:db8:a11::/48 }; }")
	require.Contains(t, doc, "set blocked_addrs { type ipv4_addr; flags interval; elements = { 203.0.113.0/24 }; }")
	require.Contains(t, doc, "set blocked_addrs6 { type ipv6_addr; flags interval; elements = { 2001:db8:bad::/48 }; }")

	// Parallel v6 match rules.
	require.Contains(t, doc, "ip6 saddr @blocked_addrs6 drop")
	require.Contains(t, doc, "ip6 saddr @mgmt_addrs6 tcp dport 22 accept")
	require.Contains(t, doc, "ip saddr 10.4.0.0/24 tcp dport @in_cluster_ports accept")
	require.Contains(t, doc, "ip6 saddr 2001:db8:c0de::/64 tcp dport @in_cluster_ports accept")

	// ICMPv6 Neighbor Discovery + MLD are mandatory under the default-drop policy
	// or IPv6 is dead; packet-too-big is the v6 PMTUD signal. nd-redirect must NOT
	// be accepted (on-link MITM). Hop-limit 255 guards the NDP accept.
	require.Contains(t, doc, "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit, nd-router-advert } ip6 hoplimit 255 accept")
	require.Contains(t, doc, "icmpv6 type { mld-listener-query, mld-listener-report, mld-listener-done } accept")
	require.Contains(t, doc, "icmpv6 type { packet-too-big, destination-unreachable, time-exceeded, parameter-problem, echo-request } accept")
	require.NotContains(t, doc, "nd-redirect")

	// NDP must be reached before the conntrack fast-path, same reasoning as the
	// v4 ICMP ordering: the NDP accepts live in the ICMPv6 chain, and the
	// dispatch into it precedes the ct vmap in the base chain. The invalid drop
	// still leads that chain, preserving the pre-split relative order.
	// Assert both anchors are present before comparing offsets: a missing rule
	// yields index -1, which would satisfy the Less() check vacuously and let
	// the invalid drop be deleted without failing this test.
	icmp6 := chainBody(t, doc, "input_icmp_ipv6")
	require.Contains(t, icmp6, "ct state invalid drop")
	require.Contains(t, icmp6, "nd-neighbor-solicit")
	require.Less(t, strings.Index(icmp6, "ct state invalid drop"), strings.Index(icmp6, "nd-neighbor-solicit"),
		"invalid drop must still precede the NDP accepts")

	base := chainBody(t, doc, "input")
	icmpDispatchIdx := strings.Index(base, "meta l4proto vmap { icmp : jump input_icmp_ipv4, icmpv6 : jump input_icmp_ipv6 }")
	ctIdx := strings.Index(base, "ct state vmap { established : accept, related : accept, invalid : drop }")
	require.Greater(t, icmpDispatchIdx, -1)
	require.Less(t, icmpDispatchIdx, ctIdx, "ICMPv6 dispatch must precede the conntrack fast-path")
}

func TestRoundTrip_RenderParseRender(t *testing.T) {
	cases := map[string]*Table{
		"full":       sampleTable(),
		"dual-stack": dualStackTable(),
		"defaults":   NewTable(),
		"mgmt-only":  {MgmtCIDRs: []string{"10.1.0.0/16"}, SSHPort: 2222},
		"v6-only":    {MgmtCIDRs: []string{"2001:db8:a11::/48"}, BlockedCIDRs: []string{"2001:db8:bad::/48"}, SSHPort: 22, PodCIDR6: "2001:db8:c0de::/64", InClusterPorts: []int{6443}},
		"no-mgmt":    {SSHPort: 22},
	}
	for name, tbl := range cases {
		t.Run(name, func(t *testing.T) {
			first, err := tbl.Render()
			require.NoError(t, err)

			parsed, err := Parse(first)
			require.NoError(t, err)

			second, err := parsed.Render()
			require.NoError(t, err)

			require.Equal(t, first, second, "render→parse→render must be the identity")
		})
	}
}

func TestRender_RejectsInjection(t *testing.T) {
	tbl := NewTable()
	tbl.MgmtCIDRs = []string{"10.0.0.0/8; reboot"}
	_, err := tbl.Render()
	require.Error(t, err)
}

func TestManager_CreateIsCreateIfMissing(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()

	// First create writes the file and triggers a service restart.
	changed, err := m.Create(ctx, sampleTable(), false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 1, applyCount)
	require.FileExists(t, nftPath)

	// The on-disk file uses the delete+recreate prefix so set elements are fully
	// cleared on every re-apply (flush table only clears chain rules, not sets).
	data, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "add table inet weaver-host-firewall")
	require.Contains(t, string(data), "delete table inet weaver-host-firewall")

	// Second create without --force is a no-op (table now exists).
	changed, err = m.Create(ctx, sampleTable(), false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, 1, applyCount)

	// With --force it re-renders and restarts again.
	changed, err = m.Create(ctx, sampleTable(), true)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 2, applyCount)
}

func TestManager_AddRemoveSet(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()

	_, err := m.Create(ctx, NewTable(), false)
	require.NoError(t, err)

	require.NoError(t, m.AddMgmtCIDR(ctx, "10.5.0.0/16"))
	data, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "10.5.0.0/16")

	require.NoError(t, m.RemoveMgmtCIDR(ctx, "10.5.0.0/16"))
	data, err = os.ReadFile(nftPath)
	require.NoError(t, err)
	require.NotContains(t, string(data), "10.5.0.0/16")

	require.NoError(t, m.AddBlockedCIDR(ctx, "203.0.113.0/24"))
	data, err = os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "203.0.113.0/24")

	require.NoError(t, m.RemoveBlockedCIDR(ctx, "203.0.113.0/24"))
	data, err = os.ReadFile(nftPath)
	require.NoError(t, err)
	require.NotContains(t, string(data), "203.0.113.0/24")

	require.NoError(t, m.Set(ctx, []string{"172.16.0.0/12"}, []string{"198.51.100.0/24"}, []int{9100}))
	data, err = os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "172.16.0.0/12")
	require.Contains(t, string(data), "198.51.100.0/24")
	require.Contains(t, string(data), "9100")
	// Set replaced the port list entirely.
	require.NotContains(t, string(data), "6443")
}

func TestManager_AddRejectsBadInput(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, _ := newTestManager(t, r, &applyCount)
	ctx := context.Background()
	_, err := m.Create(ctx, NewTable(), false)
	require.NoError(t, err)

	require.Error(t, m.AddMgmtCIDR(ctx, "not-a-cidr"))
	require.Error(t, m.AddPort(ctx, 70000))
	require.Error(t, m.AddBlockedCIDR(ctx, "not-a-cidr"))
	// A bare IP without a prefix length is still rejected (both families).
	require.Error(t, m.AddMgmtCIDR(ctx, "2001:db8::1"))
}

func TestManager_AddAcceptsIPv6(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()
	_, err := m.Create(ctx, NewTable(), false)
	require.NoError(t, err)

	// IPv6 CIDRs are now accepted and land in the ipv6_addr sets.
	require.NoError(t, m.AddMgmtCIDR(ctx, "2001:db8:a11::/48"))
	require.NoError(t, m.AddBlockedCIDR(ctx, "2001:db8:bad::/48"))
	data, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "set mgmt_addrs6 { type ipv6_addr; flags interval; elements = { 2001:db8:a11::/48 }; }")
	require.Contains(t, string(data), "set blocked_addrs6 { type ipv6_addr; flags interval; elements = { 2001:db8:bad::/48 }; }")
}

func TestTable_Validate_AcceptsIPv6(t *testing.T) {
	mgmt := sampleTable()
	mgmt.MgmtCIDRs = []string{"2001:db8::/32"}
	require.NoError(t, mgmt.Validate())

	pod := sampleTable()
	pod.PodCIDR6 = "2001:db8::/32"
	require.NoError(t, pod.Validate())

	blocked := sampleTable()
	blocked.BlockedCIDRs = []string{"2001:db8::/32"}
	require.NoError(t, blocked.Validate())

	// A bare IPv6 address without a prefix length is still rejected.
	bare := sampleTable()
	bare.MgmtCIDRs = []string{"2001:db8::1"}
	require.Error(t, bare.Validate())
}

func TestManager_MutateBeforeCreateFails(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, _ := newTestManager(t, r, &applyCount)
	require.Error(t, m.AddMgmtCIDR(context.Background(), "10.0.0.0/8"))
}

func TestManager_DeleteIsIdempotent(t *testing.T) {
	r := &fakeRunner{}
	applyCount := 0
	m, nftPath := newTestManager(t, r, &applyCount)
	ctx := context.Background()

	_, err := m.Create(ctx, sampleTable(), false)
	require.NoError(t, err)
	require.FileExists(t, nftPath)

	require.NoError(t, m.Delete(ctx))
	require.True(t, r.deleted)
	require.NoFileExists(t, nftPath)

	// Deleting again is a no-op, not an error.
	require.NoError(t, m.Delete(ctx))
}

func TestManager_ServiceFailureReturnsError(t *testing.T) {
	r := &fakeRunner{}
	dir := t.TempDir()
	m := NewManagerWithConfig(Config{
		Runner:   r,
		NftPath:  filepath.Join(dir, "network-weaver-host-firewall.nft"),
		LockPath: filepath.Join(dir, ".applying"),
		ApplyViaService: func(context.Context) error {
			return context.DeadlineExceeded
		},
	})
	_, err := m.Create(context.Background(), sampleTable(), false)
	require.Error(t, err)
}

func TestRender_GoldenStable(t *testing.T) {
	// Guards against accidental rule reordering/whitespace drift. If the
	// ruleset legitimately changes, regenerate
	// testdata/network-weaver-host-firewall.golden.nft deliberately and review
	// the diff.
	goldenPath := filepath.Join("testdata", "network-weaver-host-firewall.golden.nft")
	doc, err := sampleTable().Render()
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(goldenPath, []byte(doc), 0o644))
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(want)), strings.TrimSpace(doc))
}
