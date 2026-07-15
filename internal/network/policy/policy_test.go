// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeRunner is an in-memory Runner for tests: it records the applied document
// and the elements added per set without touching the kernel. Apply clears
// elements to mirror the real `nft -f` delete+recreate of the whole table --
// without that, tests wouldn't exercise the membership loss Manager.Create's
// snapshot/restore is meant to prevent.
type fakeRunner struct {
	applied     string
	applyCount  int
	elements    map[string][]string
	exists      bool
	applyErr    error
	listElemErr error
}

func newFakeRunner() *fakeRunner { return &fakeRunner{elements: map[string][]string{}} }

func (f *fakeRunner) Apply(_ context.Context, doc string) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = doc
	f.applyCount++
	f.exists = true
	f.elements = map[string][]string{}
	return nil
}
func (f *fakeRunner) AddElements(_ context.Context, set string, elements []string) error {
	if !f.exists {
		// Mirrors the real `nft add element` against a missing table:
		// "No such file or directory", not a silent success.
		return errors.New("nft add element " + set + " failed: No such file or directory")
	}
	f.elements[set] = append(f.elements[set], elements...)
	return nil
}
func (f *fakeRunner) DeleteElements(_ context.Context, set string, elements []string) error {
	if !f.exists {
		return errors.New("nft delete element " + set + " failed: No such file or directory")
	}
	toDelete := make(map[string]bool, len(elements))
	for _, e := range elements {
		toDelete[e] = true
	}
	current := f.elements[set]
	filtered := current[:0]
	for _, e := range current {
		if !toDelete[e] {
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
	nftPath := filepath.Join(dir, "network-weaver.nft")
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
		{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Direction: DirectionIngress, Ports: []string{"40840"}, CreatedAt: at},
		{Name: "bn-restricted", Action: ActionDeny, CreatedAt: at},
		{Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress", Direction: DirectionIngress, FromEntityWorld: true, Ports: []string{"40980", "40981"}, CreatedAt: at},
	}
}

func TestRender_GoldenMatchesBNInstallSet(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), "10.4.0.0/24")
	require.NoError(t, err)

	want, err := os.ReadFile("testdata/network-weaver.golden.nft")
	require.NoError(t, err)
	require.Equal(t, string(want), doc, "render drifted from golden; if intentional, regenerate testdata/network-weaver.golden.nft")
}

func TestRender_DeterministicRegardlessOfInputOrder(t *testing.T) {
	sorted := sampleBNPolicies()
	reversed := make([]*Policy, len(sorted))
	for i, p := range sorted {
		reversed[len(sorted)-1-i] = p
	}
	require.NotEqual(t, sorted, reversed, "test fixture must not already be a palindrome")

	want, err := Render(sorted, "10.4.0.0/24")
	require.NoError(t, err)
	got, err := Render(reversed, "10.4.0.0/24")
	require.NoError(t, err)
	require.Equal(t, want, got, "Render must sort internally, not rely on the caller's order")
}

func TestRender_TierOrderInvariants(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), "10.4.0.0/24")
	require.NoError(t, err)

	// Quarantine drops and the reply restore must both precede `ct state
	// established,related accept`, otherwise open connections survive a
	// quarantine and reply packets are misclassified.
	estIdx := strings.Index(doc, "ct state established,related accept")
	require.Positive(t, estIdx)
	require.Less(t, strings.Index(doc, "ip saddr @bn-restricted drop"), estIdx, "deny must precede est,rel accept")
	require.Less(t, strings.Index(doc, "ct direction reply ct mark 0x20"), estIdx, "reply restore must precede est,rel accept")

	// Specific (partner) must precede the fallthrough (public) so partner-bound
	// replies hit 1:40 and everyone else 1:50.
	require.Less(t, strings.Index(doc, "@bn-partner-out"), strings.Index(doc, "@bn-public-out_ports"))

	// Chain must default-drop and end with a trailing drop.
	require.Contains(t, doc, "policy drop;")
	require.True(t, strings.HasSuffix(strings.TrimSpace(doc), "}\n}") || strings.Contains(doc, "\n\t\tdrop\n"))
}

func TestRender_WorkedExamples(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), "10.4.0.0/24")
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

func TestRender_RequiresPodCIDR(t *testing.T) {
	_, err := Render(sampleBNPolicies(), "")
	require.Error(t, err)
}

func TestRender_DenyOnlyDoesNotRequirePodCIDR(t *testing.T) {
	deny := []*Policy{{Name: "bn-restricted", Action: ActionDeny, CreatedAt: fixedTime()}}
	doc, err := Render(deny, "")
	require.NoError(t, err, "a deny-only chain never references POD_CIDR")
	require.Contains(t, doc, "ip saddr @bn-restricted drop")
}

func TestCreate_PersistsAndSeedsMembership(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)
	p := &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}

	changed, err := m.Create(context.Background(), p, []string{"10.1.0.1/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)
	require.True(t, changed)

	// Registry file written; .nft persisted; initial membership applied live.
	require.FileExists(t, filepath.Join(regDir, "bn-publisher.json"))
	require.FileExists(t, nftPath)
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"])

	// Persisted .nft must NOT contain the membership element.
	onDisk, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.NotContains(t, string(onDisk), "10.1.0.1/32")
}

func TestCreate_ExistingWithoutForceIsNoOp(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)
	require.Equal(t, 1, r.applyCount)

	// Different ports AND new cidrs, but no --force: must be a pure no-op,
	// matching internal/network/firewall's create-if-missing convention.
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840", "50000"}},
		[]string{"10.1.0.2/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)
	require.False(t, changed, "an existing policy without --force must not change")
	require.Equal(t, 1, r.applyCount, "no --force on an existing policy must never re-render")
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"], "membership must be untouched")
}

func TestCreate_ForceReplacesConfigAndMembership(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-mgmt-in", Action: ActionStamp, Stamp: "reserve-ingress", Ports: []string{"40983"}, CreatedAt: fixedTime()},
		[]string{"10.0.0.0/8"}, "10.4.0.0/24", false)
	require.NoError(t, err)

	// --force with a changed port set and a different --cidrs: re-renders,
	// replaces (not merges) membership, and keeps the original created_at.
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-mgmt-in", Action: ActionStamp, Stamp: "reserve-ingress", Ports: []string{"40983", "40984"}},
		[]string{"192.168.0.0/16"}, "10.4.0.0/24", true)
	require.NoError(t, err)
	require.True(t, changed)

	got, err := readEntry(regDir, "bn-mgmt-in")
	require.NoError(t, err)
	require.Equal(t, fixedTime(), got.CreatedAt)
	require.Equal(t, []string{"40983", "40984"}, got.Ports)

	doc, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(doc), "40984")
	require.Equal(t, []string{"192.168.0.0/16"}, r.elements["bn-mgmt-in"],
		"--force replaces membership with exactly what's passed, not a merge with what was live before")
}

func TestCreate_ForceWithoutCIDRsClearsMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)

	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		nil, "10.4.0.0/24", true)
	require.NoError(t, err)
	require.Empty(t, r.elements["bn-publisher"], "--force without --cidrs replaces membership with nothing")
}

func TestCreate_PreservesSiblingMembershipAcrossRerender(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"])

	// Creating a second, different (brand-new) policy forces a full
	// re-render, which applies `delete table; add table` --
	// bn-publisher's live membership must survive that, not just its
	// rendered rule.
	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-partner-out", Action: ActionStamp, Stamp: "partner", Ports: []string{"40980"}},
		[]string{"10.20.0.0/16"}, "10.4.0.0/24", false)
	require.NoError(t, err)

	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"],
		"a sibling create must not wipe bn-publisher's live membership")
	require.Equal(t, []string{"10.20.0.0/16"}, r.elements["bn-partner-out"])
}

func TestCreate_SelfHealsMissingTableWithoutForce(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)
	require.Equal(t, 1, r.applyCount)

	// Simulate `nft delete table inet weaver` (or a reboot): the registry
	// still says bn-publisher exists, but the live kernel table is gone.
	require.NoError(t, r.Delete(context.Background()))

	// Re-run WITHOUT --force, deliberately with DIFFERENT flags/cidrs: since
	// no --force was given, those must be ignored -- only the
	// already-registered config is restored (self-heal, not a config
	// change). Membership can't be recovered this way (it was never
	// persisted), so it comes back empty.
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840", "50000"}},
		[]string{"10.1.0.99/32"}, "10.4.0.0/24", false)
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
		[]string{"10.1.0.1/32"}, "10.4.0.0/24", false)
	require.NoError(t, err)
	require.Equal(t, 1, r.applyCount)

	r.listElemErr = errors.New("nft: permission denied")
	_, err = m.Create(context.Background(),
		&Policy{Name: "bn-partner-out", Action: ActionStamp, Stamp: "partner", Ports: []string{"40980"}},
		nil, "10.4.0.0/24", false)
	require.ErrorContains(t, err, "failed to snapshot live membership")
	require.Equal(t, 1, r.applyCount, "a snapshot failure must abort before the kernel is touched")
}

func TestCreate_DenyDoesNotRequirePodCIDR(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny}, []string{"10.99.0.0/16"}, "", false)
	require.NoError(t, err, "a deny-only policy must not require a pod CIDR")
	require.True(t, changed)
	require.Equal(t, []string{"10.99.0.0/16"}, r.elements["bn-restricted"])
}

func TestCreate_DenyRecoversPodCIDRFromExistingNftFile(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
		nil, "10.4.0.0/24", false)
	require.NoError(t, err)

	// bn-restricted's own --deny rule never needs a pod CIDR, but the merged
	// chain still includes bn-publisher's rule, which does. No --pod-cidr is
	// passed here: it must be recovered from network-weaver.nft instead of
	// erroring, mirroring internal/network/firewall's Parse().
	changed, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny}, []string{"10.99.0.0/16"}, "", false)
	require.NoError(t, err, "must recover the pod CIDR from the existing network-weaver.nft artifact")
	require.True(t, changed)

	doc, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(doc), "10.4.0.0/24", "bn-publisher's rule must still be rendered with the recovered pod CIDR")
}

func TestCreate_StampSiblingStillRequiresPodCIDRWhenNftFileMissing(t *testing.T) {
	r := newFakeRunner()
	m, _, regDir := newTestManager(t, r)

	// A stamp policy already in the registry, but network-weaver.nft was
	// never written (e.g. deleted independently of the JSON registry) --
	// there is nothing to recover a pod CIDR from.
	require.NoError(t, writeEntry(regDir, &Policy{
		Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}, CreatedAt: fixedTime(),
	}))

	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny}, []string{"10.99.0.0/16"}, "", false)
	require.ErrorContains(t, err, "pod CIDR is required")
}

func TestCreate_UnknownClassRejected(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(), &Policy{Name: "bad", Action: ActionStamp, Stamp: "nonexistent"}, nil, "10.4.0.0/24", false)
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

	_, err := m.Create(context.Background(), &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}, nil, "10.4.0.0/24", false)
	require.ErrorContains(t, err, "corrupt policy registry entry")
	require.Empty(t, r.applied, "a corrupt sibling entry must fail before the kernel is touched")
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
