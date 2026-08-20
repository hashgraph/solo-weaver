// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// readNft returns the persisted artifact's contents.
func readNft(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// seedPublisher registers a managed-ports publisher policy with no initial
// membership, so each test drives membership purely through the verb it covers.
func seedPublisher(t *testing.T, m *Manager) {
	t.Helper()
	_, err := m.Create(context.Background(), &Policy{
		Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher",
		Direction: DirectionIngress, ManagedPorts: true,
	}, nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
}

func TestApplySets_PersistsMembershipToArtifact(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	applied, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32", "10.1.0.2/32"}},
		map[string][]string{"bn-publisher": {"40840"}})
	require.NoError(t, err)
	require.True(t, applied)

	// Both dimensions land in the artifact the boot oneshot replays, in the
	// collapsed form nft prints for a /32.
	doc := readNft(t, nftPath)
	require.Contains(t, doc, "set bn-publisher { type ipv4_addr; flags interval; elements = { 10.1.0.1, 10.1.0.2 }; }")
	require.Contains(t, doc, "set bn-publisher_ports { type inet_service; elements = { 40840 }; }")
}

func TestApplySets_ClearedSetRendersWithoutElements(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	_, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32"}}, nil)
	require.NoError(t, err)
	require.Contains(t, readNft(t, nftPath), "elements = { 10.1.0.1 }")

	// A category the BN stops reporting collapses to an empty set. The artifact
	// must follow, or a reboot would resurrect a peer statusz has dropped.
	_, err = m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {}}, nil)
	require.NoError(t, err)

	doc := readNft(t, nftPath)
	require.Contains(t, doc, "set bn-publisher { type ipv4_addr; flags interval; }")
	require.NotContains(t, doc, "10.1.0.1")
}

func TestApplySets_ReplayingArtifactReproducesLiveSets(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	_, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32", "10.1.0.9/32"}},
		map[string][]string{"bn-publisher": {"40840"}})
	require.NoError(t, err)

	live := map[string][]string{
		"bn-publisher":       append([]string(nil), r.elements["bn-publisher"]...),
		"bn-publisher_ports": append([]string(nil), r.elements["bn-publisher_ports"]...),
	}

	// This is the reboot: the table is gone and the oneshot replays the file.
	require.NoError(t, r.Delete(context.Background()))
	require.Empty(t, r.elements)
	require.NoError(t, r.Apply(context.Background(), readNft(t, nftPath)))

	// Compared in canonical space: the fake stores elements as the caller spelled
	// them, whereas the document (and a real `nft list set`) collapses a /32 to
	// the bare address. The property under test is which addresses are members,
	// not how each side happens to spell them.
	require.Equal(t, CanonicalizeElements(live["bn-publisher"]), CanonicalizeElements(r.elements["bn-publisher"]),
		"replaying the artifact must reproduce the membership that was live before the reboot")
	require.Equal(t, CanonicalizeElements(live["bn-publisher_ports"]), CanonicalizeElements(r.elements["bn-publisher_ports"]),
		"replaying the artifact must reproduce the listener ports that were live before the reboot")
}

func TestApplySets_UnchangedMembershipDoesNotRewriteArtifact(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	desired := map[string][]string{"bn-publisher": {"10.1.0.1/32"}}
	_, err := m.ApplySets(context.Background(), desired, nil)
	require.NoError(t, err)

	before, err := os.Stat(nftPath)
	require.NoError(t, err)
	first := readNft(t, nftPath)

	// Re-applying identical membership must be inert on disk: the SHA-256 skip
	// keeps a steady-state roster from rewriting /etc on every forced resync.
	_, err = m.ApplySets(context.Background(), desired, nil)
	require.NoError(t, err)

	after, err := os.Stat(nftPath)
	require.NoError(t, err)
	require.Equal(t, first, readNft(t, nftPath))
	require.Equal(t, before.ModTime(), after.ModTime(),
		"an unchanged render must not rewrite the artifact")
}

func TestOperatorVerbs_PersistMembershipToArtifact(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Manager) error
		want   string
	}{
		{
			name: "set",
			mutate: func(m *Manager) error {
				return m.Set(context.Background(), "bn-publisher", []string{"10.1.0.5/32"})
			},
			want: "elements = { 10.1.0.5 }",
		},
		{
			name: "add",
			mutate: func(m *Manager) error {
				return m.Add(context.Background(), "bn-publisher", []string{"10.1.0.5/32"})
			},
			want: "elements = { 10.1.0.5 }",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFakeRunner()
			m, nftPath, _ := newTestManager(t, r)
			seedPublisher(t, m)

			require.NoError(t, tc.mutate(m))

			// A hand-run membership change has to survive a reboot too, or the
			// operator's edit silently reverts on the next boot.
			require.Contains(t, readNft(t, nftPath), tc.want)
		})
	}
}

func TestRemove_PersistsMembershipToArtifact(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	require.NoError(t, m.Set(context.Background(), "bn-publisher", []string{"10.1.0.5/32", "10.1.0.6/32"}))
	require.NoError(t, m.Remove(context.Background(), "bn-publisher", []string{"10.1.0.5/32"}))

	doc := readNft(t, nftPath)
	require.Contains(t, doc, "elements = { 10.1.0.6 }")
	require.NotContains(t, doc, "10.1.0.5")
}

func TestCreate_RerenderKeepsSiblingMembershipInArtifact(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)
	require.NoError(t, m.Set(context.Background(), "bn-publisher", []string{"10.1.0.5/32"}))

	// Creating an unrelated policy re-renders the whole document. The sibling's
	// membership must come through both the kernel re-apply and the artifact.
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny},
		[]string{"10.99.0.0/16"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	require.Equal(t, []string{"10.1.0.5"}, r.elements["bn-publisher"])
	doc := readNft(t, nftPath)
	require.Contains(t, doc, "set bn-publisher { type ipv4_addr; flags interval; elements = { 10.1.0.5 }; }")
	require.Contains(t, doc, "set bn-restricted { type ipv4_addr; flags interval; elements = { 10.99.0.0/16 }; }")
}

func TestDelete_RerenderKeepsRemainingMembershipInArtifact(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)
	require.NoError(t, m.Set(context.Background(), "bn-publisher", []string{"10.1.0.5/32"}))
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-restricted", Action: ActionDeny},
		[]string{"10.99.0.0/16"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	require.NoError(t, m.Delete(context.Background(), "bn-restricted"))

	require.Equal(t, []string{"10.1.0.5"}, r.elements["bn-publisher"])
	doc := readNft(t, nftPath)
	require.Contains(t, doc, "set bn-publisher { type ipv4_addr; flags interval; elements = { 10.1.0.5 }; }")
	require.NotContains(t, doc, "bn-restricted")
}

func TestPersistMembership_RecoversPodCIDRFromLiveTableWhenArtifactIsGone(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	// An operator deletes the artifact while the table stays live. The pod CIDR
	// is only recorded in the rendered document, so without a fallback the next
	// reconcile renders a stamp policy with no pod CIDR and hard-errors — after
	// the kernel write has already landed, leaving the daemon retrying forever.
	require.NoError(t, os.Remove(nftPath))

	applied, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32"}}, nil)
	require.NoError(t, err, "a missing artifact must not fail a reconcile whose kernel write succeeded")
	require.True(t, applied)

	// The artifact is rebuilt, with the pod CIDR recovered from the live table.
	doc := readNft(t, nftPath)
	require.Contains(t, doc, "10.4.0.0/24", "pod CIDR must be recovered from the live table")
	require.Contains(t, doc, "elements = { 10.1.0.1 }")
}

func TestApplySets_NoDeltasStillRefreshesTheArtifact(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	// Put membership in the kernel while the artifact stays bare — exactly the
	// state a node lands in when the write failed once (read-only /etc) or when
	// this build is installed on an already-converged node.
	require.NoError(t, r.SetElements(context.Background(), "bn-publisher", []string{"10.1.0.1/32"}))
	require.NoError(t, os.WriteFile(nftPath, []byte(mustRender(t, m, nil)), 0o644))
	require.NotContains(t, readNft(t, nftPath), "10.1.0.1")

	// A reconcile that finds nothing to change must still re-persist. Before the
	// fix this returned early and the artifact stayed stale forever, with every
	// tick — including the hourly force-resync — reporting success.
	applied, err := m.ApplySets(context.Background(), nil, nil)
	require.NoError(t, err)
	require.True(t, applied)

	require.Contains(t, readNft(t, nftPath), "elements = { 10.1.0.1 }",
		"a no-delta reconcile must still bring the artifact up to date with the kernel")
}

// mustRender renders the current registry with the given membership, for tests
// that need to plant a specific on-disk artifact.
func mustRender(t *testing.T, m *Manager, membership map[string][]string) string {
	t.Helper()
	policies, err := loadAll(m.registryDir)
	require.NoError(t, err)
	doc, err := Render(policies, membership, "10.4.0.0/24")
	require.NoError(t, err)
	return doc
}

func TestPersistMembership_WriteFailureDoesNotFailTheReconcile(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	// Stand in for the read-only /etc a live node hit (the daemon's
	// ProtectSystem=strict namespace makes /etc/solo-provisioner read-only for
	// its sudo'd children). Point the artifact at a path whose parent is a
	// regular file: atomicWriteFile's MkdirAll then fails with ENOTDIR. Chmod
	// would not do — these tests run as root in CI, and root bypasses directory
	// permission bits.
	blocker := filepath.Join(filepath.Dir(nftPath), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o644))
	m.weaverNftPath = filepath.Join(blocker, "network-weaver-workload-policy.nft")

	// The kernel write is the reconcile's job and it succeeds; failing to persist
	// must not propagate, or superviseResponsibility faults the daemon's poll
	// loop and retries forever against a node whose kernel state is already
	// correct.
	applied, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32"}}, nil)
	require.NoError(t, err, "a failed artifact write must not fail the reconcile")
	require.True(t, applied)

	// The kernel really did get the membership.
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"])

	// And the underlying failure is still observable, not silently discarded.
	require.Error(t, m.renderAndWriteArtifact(context.Background()))
}

func TestApplySets_DualStackMembershipRoutesToBothFamilySets(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	_, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32", "2001:db8::1/128"}}, nil)
	require.NoError(t, err)

	doc := readNft(t, nftPath)
	require.Contains(t, doc, "set bn-publisher { type ipv4_addr; flags interval; elements = { 10.1.0.1 }; }")
	require.Contains(t, doc, "set bn-publisher6 { type ipv6_addr; flags interval; elements = { 2001:db8::1 }; }")
}

func TestApplySets_CompoundSetMembershipPersists(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(), &Policy{
		Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress",
		ReplyStamp: "backfill-response", Direction: DirectionEgress,
	}, nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	_, err = m.ApplySets(context.Background(),
		map[string][]string{"bn-backfill": {"10.30.5.7:43473"}}, nil)
	require.NoError(t, err)

	// A compound set carries `<ip> . <port>` keys, which must round-trip through
	// the artifact intact — the " . " separator is inside the element, not a
	// list separator.
	doc := readNft(t, nftPath)
	require.Contains(t, doc, "set bn-backfill { type ipv4_addr . inet_service; elements = { 10.30.5.7 . 43473 }; }")

	require.NoError(t, r.Delete(context.Background()))
	require.NoError(t, r.Apply(context.Background(), doc))
	require.Equal(t, []string{"10.30.5.7 . 43473"}, r.elements["bn-backfill"])
}

func TestPersistMembership_ArtifactStaysLoadableAfterRepeatedApplies(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPublisher(t, m)

	for _, cidrs := range [][]string{
		{"10.1.0.1/32"},
		{"10.1.0.1/32", "10.1.0.2/32"},
		{"10.1.0.2/32"},
		{},
	} {
		_, err := m.ApplySets(context.Background(), map[string][]string{"bn-publisher": cidrs}, nil)
		require.NoError(t, err)
	}

	// The table prefix must survive every rewrite, or the boot oneshot would
	// fail to load the document at all.
	doc := readNft(t, nftPath)
	require.True(t, strings.HasPrefix(doc, "add table "+TableName+"\ndelete table "+TableName+"\nadd table "+TableName+"\n"),
		"the idempotent table prefix must be preserved across membership rewrites")
	require.Contains(t, doc, "set bn-publisher { type ipv4_addr; flags interval; }")
}
