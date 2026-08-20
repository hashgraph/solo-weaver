// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// update regenerates the golden .nft fixtures instead of comparing against
// them: `go test ./internal/network/policy/... -update`. Review the diff before
// committing a regenerated golden.
var update = flag.Bool("update", false, "regenerate golden testdata files")

// fakeRunner is an in-memory Runner for tests: it records the applied document
// and the elements added per set without touching the kernel. Apply mirrors the
// real `nft -f` delete+recreate of the whole table -- every set is emptied, then
// re-seeded from the `elements = { … }` clauses the document declares. Both
// halves matter: dropping the seeding would hide the membership persistence
// this package now relies on, and dropping the clearing would hide the loss it
// protects against.
type fakeRunner struct {
	applied      string
	applyCount   int
	elements     map[string][]string
	exists       bool
	applyErr     error
	listElemErr  error
	setElemOrder []string // set names in the order SetElements was called
}

func newFakeRunner() *fakeRunner { return &fakeRunner{elements: map[string][]string{}} }

func (f *fakeRunner) Apply(_ context.Context, doc string) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = doc
	f.applyCount++
	f.exists = true
	f.elements = parseDocElements(doc)
	return nil
}

// reDocSetElements matches one set declaration carrying inline elements in a
// rendered document, capturing the set name and the raw element list.
var reDocSetElements = regexp.MustCompile(`(?m)^\tset (\S+) \{ type .*?elements = \{ ([^}]*) \}; \}$`)

// parseDocElements recovers the per-set membership a rendered document seeds,
// so the fake table comes up populated exactly as `nft -f` would leave it.
func parseDocElements(doc string) map[string][]string {
	out := map[string][]string{}
	for _, m := range reDocSetElements.FindAllStringSubmatch(doc, -1) {
		var elements []string
		for _, e := range strings.Split(m[2], ",") {
			if e = strings.TrimSpace(e); e != "" {
				elements = append(elements, e)
			}
		}
		if len(elements) > 0 {
			out[m[1]] = elements
		}
	}
	return out
}

func (f *fakeRunner) AddElements(_ context.Context, set string, elements []string) error {
	if !f.exists {
		// Mirrors the real `nft add element` against a missing table:
		// "No such file or directory", not a silent success.
		return errors.New("nft add element " + set + " failed: No such file or directory")
	}
	if err := rejectConflictingIntervals(set, append(append([]string(nil), f.elements[set]...), elements...)); err != nil {
		return err
	}
	f.elements[set] = append(f.elements[set], elements...)
	return nil
}

// rejectMissingElements mirrors `nft delete element` refusing to delete an
// element the set does not hold. Comparison is on canonical form so a member
// stored as the bare "10.0.0.7" is still deletable as "10.0.0.7/32", which is
// what the real kernel does.
func rejectMissingElements(set string, current, toDelete []string) error {
	have := make(map[string]struct{}, len(current))
	for _, e := range current {
		have[parseElement(e).canon] = struct{}{}
	}
	for _, e := range toDelete {
		if _, ok := have[parseElement(e).canon]; !ok {
			return errors.New("nft delete element " + set + " failed: Error: element does not exist")
		}
	}
	return nil
}

// rejectConflictingIntervals mirrors what the real kernel does to a `flags
// interval` set (which is how renderSetDecls declares every policy address set)
// when two elements overlap: nft refuses the whole transaction with
// "conflicting intervals specified". Verified against nftables v1.0.6 --
// containment is rejected, while adjacent prefixes and exact duplicates are
// accepted. Without this the fake would happily store membership the kernel
// would refuse, and a code path that skipped the Go-side containment check
// (cidrset.go) would pass its tests and only fail on a real host.
func rejectConflictingIntervals(set string, elements []string) error {
	if outer, inner, found := containmentPair(elements); found {
		return errors.New("nft add element " + set + " failed: Error: conflicting intervals specified: " + outer + " and " + inner)
	}
	return nil
}
func (f *fakeRunner) DeleteElements(_ context.Context, set string, elements []string) error {
	if !f.exists {
		return errors.New("nft delete element " + set + " failed: No such file or directory")
	}
	// Mirrors the real `nft delete element`: an element that is not in the set
	// fails the WHOLE transaction and removes nothing (verified against nftables
	// v1.0.6). Without this the fake silently no-ops on an absent element, which
	// hid the fact that Manager.Remove leaked a bare "element does not exist" for
	// a covered CIDR, an unrelated non-member, and any batch containing one.
	if err := rejectMissingElements(set, f.elements[set], elements); err != nil {
		return err
	}
	// Matched on canonical form, like the kernel: an element the operator typed
	// as "10.0.0.7/32" deletes a member nft stored as the bare "10.0.0.7".
	toDelete := make(map[string]bool, len(elements))
	for _, e := range elements {
		toDelete[parseElement(e).canon] = true
	}
	current := f.elements[set]
	filtered := current[:0]
	for _, e := range current {
		if !toDelete[parseElement(e).canon] {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(f.elements, set)
	} else {
		f.elements[set] = filtered
	}
	return nil
}
func (f *fakeRunner) SetElements(_ context.Context, set string, elements []string) error {
	if !f.exists {
		return errors.New("nft flush set " + set + " failed: No such file or directory")
	}
	if err := rejectConflictingIntervals(set, elements); err != nil {
		return err
	}
	f.setElemOrder = append(f.setElemOrder, set)
	if len(elements) == 0 {
		delete(f.elements, set)
	} else {
		f.elements[set] = append([]string(nil), elements...)
	}
	return nil
}
func (f *fakeRunner) ListElements(_ context.Context, set string) ([]string, error) {
	if f.listElemErr != nil {
		return nil, f.listElemErr
	}
	return append([]string(nil), f.elements[set]...), nil
}
func (f *fakeRunner) List(context.Context) (string, error) { return f.applied, nil }
func (f *fakeRunner) Delete(context.Context) error {
	// Mirrors `nft delete table`: the table and every set in it are gone.
	f.exists = false
	f.elements = map[string][]string{}
	return nil
}
func (f *fakeRunner) Exists(context.Context) (bool, error) { return f.exists, nil }

// newTestManager wires a Manager with a fakeRunner, temp paths, and a no-op
// service func so the package runs on any platform without touching systemd.
func newTestManager(t *testing.T, r *fakeRunner) (*Manager, string, string) {
	t.Helper()
	dir := t.TempDir()
	nftPath := filepath.Join(dir, "network-weaver-workload-policy.nft")
	regDir := filepath.Join(dir, "policies")
	m := NewManagerWithConfig(Config{
		Runner:        r,
		WeaverNftPath: nftPath,
		RegistryDir:   regDir,
		LockPath:      filepath.Join(dir, ".applying"),
		EnsureService: func(context.Context) error { return nil },
	})
	return m, nftPath, regDir
}

func fixedTime() time.Time { return time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC) }

// sampleBNPolicies mirrors the BN install policy set.
func sampleBNPolicies() []*Policy {
	at := fixedTime()
	return []*Policy{
		{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response", Direction: DirectionEgress, CreatedAt: at},
		{Name: "bn-partner-out", Action: ActionStamp, Stamp: "partner", Direction: DirectionEgress, Ports: []string{"40980", "40981"}, CreatedAt: at},
		{Name: "bn-public-out", Action: ActionStamp, Stamp: "public", Direction: DirectionEgress, FromEntityWorld: true, Ports: []string{"40980", "40981"}, CreatedAt: at},
		{Name: "bn-health", Action: ActionDeny, FromEntityWorld: true, Ports: []string{"40983"}, CreatedAt: at},
		{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Direction: DirectionIngress, Ports: []string{"40840"}, CreatedAt: at},
		{Name: "bn-restricted", Action: ActionDeny, CreatedAt: at},
		{Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress", Direction: DirectionIngress, FromEntityWorld: true, Ports: []string{"40980", "40981"}, CreatedAt: at},
	}
}

func TestRender_GoldenMatchesBNInstallSet(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24")
	require.NoError(t, err)

	goldenPath := "testdata/network-weaver-workload-policy.golden.nft"
	if *update {
		require.NoError(t, os.WriteFile(goldenPath, []byte(doc), 0o644))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(want), doc, "render drifted from golden; if intentional, regenerate with -update")
}

// TestRender_DualStackGolden pins the dual-stack render: the same BN install set
// with both an IPv4 and an IPv6 pod CIDR. It asserts the v6 sets and `ip6` rules
// appear alongside their v4 counterparts.
func TestRender_DualStackGolden(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	goldenPath := "testdata/network-weaver-workload-policy-dualstack.golden.nft"
	if *update {
		require.NoError(t, os.WriteFile(goldenPath, []byte(doc), 0o644))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(want), doc, "dual-stack render drifted from golden; if intentional, regenerate with -update")

	// Spot-check the key v6 constructs are present.
	require.Contains(t, doc, "set bn-publisher6 { type ipv6_addr; flags interval; }")
	require.Contains(t, doc, "set bn-backfill6 { type ipv6_addr . inet_service; }")
	require.Contains(t, doc, "ip6 saddr @bn-restricted6 drop")
	require.Contains(t, doc, "ip6 daddr 2001:db8:c0de::/64 ip6 saddr @bn-publisher6")
}

// chainBody returns the body of the named chain, so an ordering assertion can be
// scoped to one chain. Document order stopped being evaluation order when the
// forward chain was split by family: the per-family chains are defined below the
// hooked chain but run before it falls through, via the nfproto dispatch.
func chainBody(t *testing.T, doc, name string) string {
	t.Helper()
	// An empty chain renders on one line as `chain <name> { }`; matching only the
	// multi-line form would fail here with "not found" for a chain that is present
	// and legitimately empty.
	if strings.Contains(doc, "\tchain "+name+" { }\n") {
		return ""
	}
	header := "\tchain " + name + " {\n"
	start := strings.Index(doc, header)
	require.Greater(t, start, -1, "chain %s not found in rendered document", name)
	rest := doc[start+len(header):]
	end := strings.Index(rest, "\n\t}\n")
	require.Greater(t, end, -1, "chain %s is not terminated", name)
	return rest[:end]
}

// TestRender_SingleStackEmitsAnEmptyChainForTheAbsentFamily covers the one input
// that produces a chain with no rules at all: stamp-only policies (no deny tier,
// the only tier a family without a pod CIDR renders) on a single-stack
// deployment. The chain must still exist, because the hooked chain's vmap jumps
// to it unconditionally and an unresolved jump fails the whole load.
func TestRender_SingleStackEmitsAnEmptyChainForTheAbsentFamily(t *testing.T) {
	stampOnly := []*Policy{
		{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Direction: DirectionIngress, Ports: []string{"40840"}, CreatedAt: fixedTime()},
	}
	doc, err := Render(stampOnly, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "jump "+chainV6, "the dispatch always jumps to both families")
	require.Contains(t, doc, "\tchain "+chainV6+" { }\n", "the absent family's chain must still be declared")
	require.Contains(t, chainBody(t, doc, chainV4), "ip daddr 10.4.0.0/24 ip saddr @bn-publisher")
}

// TestRender_AbsentFamilyOmitsTheReplyRestore pins the reply-restore tier to the
// same pod-CIDR gate as the stamp tiers. The ct mark it matches is only written
// by a stamp rule, which renders solely for a family that has a pod CIDR, so
// emitting the restore for the absent family would add a rule no packet can ever
// match.
func TestRender_AbsentFamilyOmitsTheReplyRestore(t *testing.T) {
	replyStamp := []*Policy{
		{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response", Direction: DirectionEgress, CreatedAt: fixedTime()},
		{Name: "bn-restricted", Action: ActionDeny, CreatedAt: fixedTime()},
	}

	single, err := Render(replyStamp, nil, "10.4.0.0/24")
	require.NoError(t, err)
	require.Contains(t, chainBody(t, single, chainV4), "ct direction reply")
	require.NotContains(t, chainBody(t, single, chainV6), "ct direction reply",
		"a family with no pod CIDR carries the deny tier only")

	dual, err := Render(replyStamp, nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)
	require.Contains(t, chainBody(t, dual, chainV6), "ct direction reply",
		"both families keep the restore once both have a pod CIDR")
}

func TestRender_DeterministicRegardlessOfInputOrder(t *testing.T) {
	sorted := sampleBNPolicies()
	reversed := make([]*Policy, len(sorted))
	for i, p := range sorted {
		reversed[len(sorted)-1-i] = p
	}
	require.NotEqual(t, sorted, reversed, "test fixture must not already be a palindrome")

	want, err := Render(sorted, nil, "10.4.0.0/24")
	require.NoError(t, err)
	got, err := Render(reversed, nil, "10.4.0.0/24")
	require.NoError(t, err)
	require.Equal(t, want, got, "Render must sort internally, not rely on the caller's order")
}

// TestRender_TierOrderInvariants pins tier order *within* a family chain. Every
// assertion is scoped to one chain body: a whole-document strings.Index compares
// positions across chains that never evaluate the same packet, which would make
// the assertion meaningless.
func TestRender_TierOrderInvariants(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	for _, tc := range []struct {
		chain, deny, specific, fallthr string
	}{
		{chainV4, "ip saddr @bn-restricted drop", "@bn-partner-out ", "@bn-public-out_ports"},
		{chainV6, "ip6 saddr @bn-restricted6 drop", "@bn-partner-out6 ", "@bn-public-out_ports"},
	} {
		t.Run(tc.chain, func(t *testing.T) {
			body := chainBody(t, doc, tc.chain)

			// The quarantine drops must lead the chain: every rule below them
			// ends in `accept`, so a deny that sorted lower would let a
			// restricted peer's traffic be stamped and accepted instead.
			denyIdx := strings.Index(body, tc.deny)
			restoreIdx := strings.Index(body, "ct direction reply ct mark 0x20")
			specificIdx := strings.Index(body, tc.specific)
			require.Positive(t, denyIdx)
			require.Positive(t, restoreIdx)
			require.Positive(t, specificIdx)
			require.Less(t, denyIdx, restoreIdx, "deny must precede the reply restore")
			require.Less(t, restoreIdx, specificIdx, "reply restore must precede classification")

			// Specific (partner) must precede the fallthrough (public) so
			// partner-bound replies hit 1:40 and everyone else 1:50.
			require.Less(t, specificIdx, strings.Index(body, tc.fallthr))
		})
	}
}

// TestRender_FamilySplit pins the structural change: a minimal hooked chain that
// only dispatches, per-family chains that carry no rule from the other family,
// and no conntrack-state rule or terminal drop anywhere.
func TestRender_FamilySplit(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	base := chainBody(t, doc, chainBase)
	require.Contains(t, base, "type filter hook forward priority 0; policy accept;")
	require.Contains(t, base, "meta nfproto vmap { ipv4 : jump forward_ipv4, ipv6 : jump forward_ipv6 }")

	// The hooked chain must carry nothing but the policy line and the dispatch —
	// a rule that creeps in here is evaluated for every forwarded packet.
	var baseRules []string
	for _, line := range strings.Split(base, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			baseRules = append(baseRules, line)
		}
	}
	require.Len(t, baseRules, 2, "hooked chain must hold only the policy line and the dispatch, got %v", baseRules)

	// No rule may match a family it cannot belong to.
	for _, line := range strings.Split(chainBody(t, doc, chainV4), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		require.NotContains(t, line, "ip6 ", "IPv6 match in the IPv4 chain: %s", line)
	}
	for _, line := range strings.Split(chainBody(t, doc, chainV6), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		require.False(t, strings.HasPrefix(line, "ip "), "IPv4 match in the IPv6 chain: %s", line)
	}

	// The reply restore matches conntrack only, so it belongs in both chains —
	// it cannot be hoisted into the hooked chain above the dispatch, where its
	// `accept` would let a quarantined peer's replies escape the deny tier.
	require.Contains(t, chainBody(t, doc, chainV4), "ct direction reply ct mark 0x20")
	require.Contains(t, chainBody(t, doc, chainV6), "ct direction reply ct mark 0x20")

	// The chain classifies; it no longer enforces.
	require.NotContains(t, doc, "policy drop;")
	require.NotContains(t, doc, "ct state")
	require.NotContains(t, doc, "\n\t\tdrop\n")
}

func TestRender_WorkedExamples(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24")
	require.NoError(t, err)

	// publisher stamp.
	require.Contains(t, doc, "ip daddr 10.4.0.0/24 ip saddr @bn-publisher tcp dport @bn-publisher_ports meta priority set 0x10010 accept")
	// from-entity world fallthrough (no @set clause).
	require.Contains(t, doc, "ip daddr 10.4.0.0/24 tcp dport @bn-subscriber-in_ports meta priority set 0x10030 accept")
	// deny (both directions).
	require.Contains(t, doc, "ip saddr @bn-restricted drop")
	require.Contains(t, doc, "ip daddr @bn-restricted drop")
	// reply-stamp compound-key forward rule + ct mark write.
	require.Contains(t, doc, "ip saddr 10.4.0.0/24 ip daddr . tcp dport @bn-backfill ct mark set 0x20 meta priority set 0x10060 accept")
	// compound set schema, no `flags interval`.
	require.Contains(t, doc, "set bn-backfill { type ipv4_addr . inet_service; }")
}

func TestRender_CreatedAtTiebreakWithinDirPortsGroup(t *testing.T) {
	older := fixedTime()
	newer := older.Add(time.Hour)

	// Both policies are egress fallthrough with the same --ports, so they
	// land in the same tier-4 group. Name order ("aaa" < "zzz") disagrees
	// with CreatedAt order here on purpose, so the assertion below can only
	// pass if renderChain is ordering by CreatedAt, not by name.
	policies := []*Policy{
		{Name: "aaa-newer", Action: ActionStamp, Stamp: "public", Direction: DirectionEgress, FromEntityWorld: true, Ports: []string{"9000"}, CreatedAt: newer},
		{Name: "zzz-older", Action: ActionStamp, Stamp: "reserve-egress", Direction: DirectionEgress, FromEntityWorld: true, Ports: []string{"9000"}, CreatedAt: older},
	}
	doc, err := Render(policies, nil, "10.4.0.0/24")
	require.NoError(t, err)

	zzzIdx := strings.Index(doc, "@zzz-older_ports")
	aaaIdx := strings.Index(doc, "@aaa-newer_ports")
	require.NotEqual(t, -1, zzzIdx, "zzz-older's rule must be present")
	require.NotEqual(t, -1, aaaIdx, "aaa-newer's rule must be present")
	require.Less(t, zzzIdx, aaaIdx, "the older policy must render first despite sorting after the newer one by name")
}

func TestRender_RequiresPodCIDR(t *testing.T) {
	_, err := Render(sampleBNPolicies(), nil, "")
	require.Error(t, err)
}

func TestRender_DenyOnlyDoesNotRequirePodCIDR(t *testing.T) {
	deny := []*Policy{{Name: "bn-restricted", Action: ActionDeny, CreatedAt: fixedTime()}}
	doc, err := Render(deny, nil, "")
	require.NoError(t, err, "a deny-only chain never references POD_CIDR")
	require.Contains(t, doc, "ip saddr @bn-restricted drop")
}

func TestCreate_PersistsAndSeedsMembership(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)
	p := &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}

	changed, err := m.Create(context.Background(), p, []string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.True(t, changed)

	// Registry file written; .nft persisted; initial membership applied live.
	require.FileExists(t, filepath.Join(regDir, "bn-publisher.json"))
	require.FileExists(t, nftPath)
	require.Equal(t, []string{"10.1.0.1"}, r.elements["bn-publisher"])

	// The persisted .nft carries the membership inline, which is what makes it
	// survive a reboot: the boot oneshot replays this document and the set comes
	// up populated. Asserted in the collapsed form nft itself prints for a /32.
	onDisk, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(onDisk), "set bn-publisher { type ipv4_addr; flags interval; elements = { 10.1.0.1 }; }")
}

func TestCreate_ExistingWithoutForceIsNoOp(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.Equal(t, 1, r.applyCount)

	// Different ports AND new cidrs, but no --force: must be a pure no-op,
	// matching internal/network/firewall's create-if-missing convention.
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840", "50000"}},
		[]string{"10.1.0.2/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.False(t, changed, "an existing policy without --force must not change")
	require.Equal(t, 1, r.applyCount, "no --force on an existing policy must never re-render")
	require.Equal(t, []string{"10.1.0.1"}, r.elements["bn-publisher"], "membership must be untouched")
}

func TestCreate_ForceReplacesConfigAndMembership(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress", Ports: []string{"40983"}, CreatedAt: fixedTime()},
		[]string{"10.0.0.0/8"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	// --force with a changed port set and a different --cidrs: re-renders,
	// replaces (not merges) membership, and keeps the original created_at.
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress", Ports: []string{"40983", "40984"}},
		[]string{"192.168.0.0/16"}, []string{"10.4.0.0/24"}, true)
	require.NoError(t, err)
	require.True(t, changed)

	got, err := readEntry(regDir, "bn-subscriber-in")
	require.NoError(t, err)
	require.Equal(t, fixedTime(), got.CreatedAt)
	require.Equal(t, []string{"40983", "40984"}, got.Ports)

	doc, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(doc), "40984")
	require.Equal(t, []string{"192.168.0.0/16"}, r.elements["bn-subscriber-in"],
		"--force replaces membership with exactly what's passed, not a merge with what was live before")
}

func TestCreate_ForceWithoutCIDRsClearsMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		nil, []string{"10.4.0.0/24"}, true)
	require.NoError(t, err)
	require.Empty(t, r.elements["bn-publisher"], "--force without --cidrs replaces membership with nothing")
}

func TestCreate_PreservesSiblingMembershipAcrossRerender(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.Equal(t, []string{"10.1.0.1"}, r.elements["bn-publisher"])

	// Creating a second, different (brand-new) policy forces a full
	// re-render, which applies `delete table; add table` --
	// bn-publisher's live membership must survive that, not just its
	// rendered rule.
	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-partner-out", Action: ActionStamp, Stamp: "partner", Ports: []string{"40980"}},
		[]string{"10.20.0.0/16"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	require.Equal(t, []string{"10.1.0.1"}, r.elements["bn-publisher"],
		"a sibling create must not wipe bn-publisher's live membership")
	require.Equal(t, []string{"10.20.0.0/16"}, r.elements["bn-partner-out"])
}

func TestCreate_SelfHealsMissingTableWithoutForce(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.Equal(t, 1, r.applyCount)

	// Simulate `nft delete table inet weaver-workload-policy` (or a reboot): the registry
	// still says bn-publisher exists, but the live kernel table is gone.
	require.NoError(t, r.Delete(context.Background()))

	// Re-run WITHOUT --force, deliberately with DIFFERENT flags/cidrs: since
	// no --force was given, those must be ignored -- only the
	// already-registered config is restored (self-heal, not a config
	// change). Membership can't be recovered this way (it was never
	// persisted), so it comes back empty.
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840", "50000"}},
		[]string{"10.1.0.99/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err, "a missing table must be restored, not error")
	require.True(t, changed)
	require.Equal(t, 2, r.applyCount, "the table must be re-rendered when the kernel table is missing")

	doc, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(doc), "40840")
	require.NotContains(t, string(doc), "50000", "without --force, new flags must not be applied, even to self-heal a missing table")
	require.Empty(t, r.elements["bn-publisher"], "membership already lost from the kernel can't be recovered without --force")
}

func TestCreate_SnapshotFailureAbortsBeforeApply(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.Equal(t, 1, r.applyCount)

	r.listElemErr = errors.New("nft: permission denied")
	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-partner-out", Action: ActionStamp, Stamp: "partner", Ports: []string{"40980"}},
		nil, []string{"10.4.0.0/24"}, false)
	require.ErrorContains(t, err, "failed to snapshot live membership")
	require.Equal(t, 1, r.applyCount, "a snapshot failure must abort before the kernel is touched")
}

func TestCreate_DenyDoesNotRequirePodCIDR(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny}, []string{"10.99.0.0/16"}, nil, false)
	require.NoError(t, err, "a deny-only policy must not require a pod CIDR")
	require.True(t, changed)
	require.Equal(t, []string{"10.99.0.0/16"}, r.elements["bn-restricted"])
}

func TestCreate_DenyRecoversPodCIDRFromExistingNftFile(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	// bn-restricted's own --deny rule never needs a pod CIDR, but the merged
	// chain still includes bn-publisher's rule, which does. No --pod-cidr is
	// passed here: it must be recovered from network-weaver-workload-policy.nft instead of
	// erroring, mirroring internal/network/firewall's Parse().
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny}, []string{"10.99.0.0/16"}, nil, false)
	require.NoError(t, err, "must recover the pod CIDR from the existing network-weaver-workload-policy.nft artifact")
	require.True(t, changed)

	doc, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(doc), "10.4.0.0/24", "bn-publisher's rule must still be rendered with the recovered pod CIDR")

	// A slice carrying only an empty string (e.g. Cobra StringSlice from
	// `--pod-cidr ""`) must be normalized away so recovery still fires, rather
	// than being treated as one explicit-but-empty pod CIDR that fails Render.
	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-partner-out", Action: ActionDeny}, []string{"10.88.0.0/16"}, []string{""}, false)
	require.NoError(t, err, "an empty --pod-cidr entry must not defeat pod-CIDR recovery")
}

func TestCreate_StampSiblingStillRequiresPodCIDRWhenNftFileMissing(t *testing.T) {
	r := newFakeRunner()
	m, _, regDir := newTestManager(t, r)

	// A stamp policy already in the registry, but network-weaver-workload-policy.nft was
	// never written (e.g. deleted independently of the JSON registry) --
	// there is nothing to recover a pod CIDR from.
	require.NoError(t, writeEntry(regDir, &Policy{
		Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}, CreatedAt: fixedTime(),
	}))

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny}, []string{"10.99.0.0/16"}, nil, false)
	require.ErrorContains(t, err, "pod CIDR is required")
}

func TestCreate_UnknownClassRejected(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(), &Policy{Name: "bad", Action: ActionStamp, Stamp: "nonexistent"}, nil, []string{"10.4.0.0/24"}, false)
	require.ErrorContains(t, err, "unknown class")
	require.Empty(t, r.applied, "invalid policy must never reach the kernel")
}

func TestCreate_CorruptSiblingRegistryRejected(t *testing.T) {
	r := newFakeRunner()
	m, _, regDir := newTestManager(t, r)
	require.NoError(t, os.MkdirAll(regDir, 0o755))

	// A hand-edited sibling entry that is invalid (references a class that no
	// longer exists in classMap).
	bad := `{"name":"bn-bad","action":"stamp","stamp":"retired-class","direction":"","ports":null,"from_entity_world":false}`
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "bn-bad.json"), []byte(bad), 0o644))

	_, err := m.Create(context.Background(), &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}, nil, []string{"10.4.0.0/24"}, false)
	require.ErrorContains(t, err, "corrupt policy registry entry")
	require.Empty(t, r.applied, "a corrupt sibling entry must fail before the kernel is touched")
}

func TestCreate_OverlappingSpecificPoliciesRejected(t *testing.T) {
	r := newFakeRunner()
	m, _, regDir := newTestManager(t, r)

	// publisher and reserve-ingress are both DirectionIngress; same --ports
	// makes the two policies claim the same (Direction, Ports) group.
	a := &Policy{Name: "bn-a", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}
	changed, err := m.Create(context.Background(), a, []string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	require.True(t, changed)
	firstApplyCount := r.applyCount

	b := &Policy{Name: "bn-b", Action: ActionStamp, Stamp: "reserve-ingress", Ports: []string{"40840"}}
	_, err = m.Create(context.Background(), b, []string{"10.1.0.2/32"}, []string{"10.4.0.0/24"}, false)
	require.ErrorContains(t, err, "overlaps with existing policy")

	require.Equal(t, firstApplyCount, r.applyCount, "a rejected overlap must never reach the kernel")
	require.NoFileExists(t, filepath.Join(regDir, "bn-b.json"))
}

func TestCreate_OverlapCheckExcludesSelfOnForce(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	a := &Policy{Name: "bn-a", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}
	_, err := m.Create(context.Background(), a, []string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	// Re-creating the SAME name with --force must not trip the overlap check
	// against its own prior registry entry.
	changed, err := m.Create(context.Background(), a, []string{"10.1.0.2/32"}, []string{"10.4.0.0/24"}, true)
	require.NoError(t, err)
	require.True(t, changed)
}

func TestCreate_FromEntityWorldNotSubjectToOverlapCheck(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	a := &Policy{Name: "bn-a", Action: ActionStamp, Stamp: "public", FromEntityWorld: true, Ports: []string{"9000"}}
	_, err := m.Create(context.Background(), a, nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	b := &Policy{Name: "bn-b", Action: ActionStamp, Stamp: "reserve-egress", FromEntityWorld: true, Ports: []string{"9000"}}
	changed, err := m.Create(context.Background(), b, nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err, "fallthrough policies sharing direction+ports are not overlap-checked")
	require.True(t, changed)

	require.Contains(t, r.applied, "@bn-a_ports")
	require.Contains(t, r.applied, "@bn-b_ports")
}

func TestRegistry_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := &Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response", Direction: DirectionEgress, CreatedAt: fixedTime()}
	require.NoError(t, writeEntry(dir, p))

	got, err := readEntry(dir, "bn-backfill")
	require.NoError(t, err)
	require.Equal(t, p, got)

	all, err := loadAll(dir)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "bn-backfill", all[0].Name)
}
