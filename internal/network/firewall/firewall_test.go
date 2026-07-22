// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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

// newTestManager wires a Manager with a fakeRunner and temp paths. The
// applyViaService closure sets r.exists = true and increments applyCount so
// tests can assert on how many times rules were applied without touching
// systemd or the kernel.
func newTestManager(t *testing.T, r *fakeRunner, applyCount *int) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-host.nft")
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
	require.Contains(t, doc, "icmp type destination-unreachable accept")
	require.Contains(t, doc, "icmp type time-exceeded accept")
	require.Contains(t, doc, "icmp type echo-request limit rate 10/second accept")
	// Over-budget echo must be dropped explicitly, not left to fall through.
	require.Contains(t, doc, "icmp type echo-request drop")
	require.Contains(t, doc, "ip saddr 10.4.0.0/24 tcp dport @in_cluster_ports accept")

	// Ordering is load-bearing: the echo-request rate limit must be evaluated
	// BEFORE `ct state established,related accept`. netfilter conntrack tracks
	// ICMP echo flows, so if the established fast-path ran first, every packet
	// of a sustained ping after the first would bypass the limit.
	limitIdx := strings.Index(doc, "icmp type echo-request limit rate 10/second accept")
	dropIdx := strings.Index(doc, "icmp type echo-request drop")
	// LastIndex: the comment above the rule also references the phrase; the real
	// rule is the last occurrence.
	estIdx := strings.LastIndex(doc, "ct state established,related accept")
	require.Greater(t, dropIdx, limitIdx, "echo drop must follow the echo rate limit")
	require.Greater(t, estIdx, dropIdx, "established,related accept must come after the ICMP echo rules")

	// The block list must be evaluated before established/related so an
	// operator-added CIDR also kills already-open connections, not just new ones.
	blockedIdx := strings.Index(doc, "ip saddr @blocked_addrs drop")
	require.Greater(t, blockedIdx, 0)
	require.Less(t, blockedIdx, estIdx, "blocked-CIDR drop must precede established,related accept")
}

func TestRender_NoMgmtNoPod(t *testing.T) {
	tbl := NewTable() // no mgmt CIDRs, no pod CIDR
	doc, err := tbl.Render()
	require.NoError(t, err)

	// Empty mgmt set renders without an elements clause; no pod CIDR means no
	// in-cluster rule line.
	require.Contains(t, doc, "set mgmt_addrs { type ipv4_addr; flags interval; }")
	require.Contains(t, doc, "set blocked_addrs { type ipv4_addr; flags interval; }")
	require.NotContains(t, doc, "tcp dport @in_cluster_ports accept")
}

func TestRoundTrip_RenderParseRender(t *testing.T) {
	cases := map[string]*Table{
		"full":      sampleTable(),
		"defaults":  NewTable(),
		"mgmt-only": {MgmtCIDRs: []string{"10.1.0.0/16"}, SSHPort: 2222},
		"no-mgmt":   {SSHPort: 22},
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
	require.Contains(t, string(data), "add table inet host")
	require.Contains(t, string(data), "delete table inet host")

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
	require.Error(t, m.AddMgmtCIDR(ctx, "2001:db8::/32")) // IPv6 not supported by ipv4_addr sets
	require.Error(t, m.AddBlockedCIDR(ctx, "not-a-cidr"))
	require.Error(t, m.AddBlockedCIDR(ctx, "2001:db8::/32")) // IPv6 not supported by ipv4_addr sets
}

func TestTable_Validate_RejectsIPv6(t *testing.T) {
	mgmt := sampleTable()
	mgmt.MgmtCIDRs = []string{"2001:db8::/32"}
	require.ErrorContains(t, mgmt.Validate(), "IPv6 CIDRs are not yet supported")

	pod := sampleTable()
	pod.PodCIDR = "2001:db8::/32"
	require.ErrorContains(t, pod.Validate(), "IPv6 CIDRs are not yet supported")

	blocked := sampleTable()
	blocked.BlockedCIDRs = []string{"2001:db8::/32"}
	require.ErrorContains(t, blocked.Validate(), "IPv6 CIDRs are not yet supported")
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
		NftPath:  filepath.Join(dir, "network-host.nft"),
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
	// ruleset legitimately changes, regenerate testdata/network-host.golden.nft
	// deliberately and review the diff.
	want, err := os.ReadFile(filepath.Join("testdata", "network-host.golden.nft"))
	require.NoError(t, err)

	doc, err := sampleTable().Render()
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(want)), strings.TrimSpace(doc))
}
