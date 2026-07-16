// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// lockPathFor derives the Manager's shared apply-lock path from the nft path
// newTestManager returns: both live in the same temp dir, and newTestManager
// wires LockPath to "<dir>/.applying".
func lockPathFor(nftPath string) string {
	return filepath.Join(filepath.Dir(nftPath), ".applying")
}

func TestApplyMembership_AppliesAllPolicies(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	seedDenyPolicy(t, m, "bn-restricted", nil)

	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-publisher":  {"10.1.0.1/32", "10.1.0.2/32"},
		"bn-restricted": {"10.99.0.0/16"},
	})
	require.NoError(t, err)
	require.True(t, applied, "a clean lock acquisition applies the batch")
	require.Equal(t, []string{"10.1.0.1/32", "10.1.0.2/32"}, r.elements["bn-publisher"])
	require.Equal(t, []string{"10.99.0.0/16"}, r.elements["bn-restricted"])
}

func TestApplyMembership_OnePolicyPerTransactionInSortedOrder(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	seedDenyPolicy(t, m, "bn-restricted", nil)
	seedDenyPolicy(t, m, "bn-partner", nil)

	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-restricted": {"10.99.0.0/16"},
		"bn-publisher":  {"10.1.0.1/32"},
		"bn-partner":    {"10.2.0.0/16"},
	})
	require.NoError(t, err)
	require.True(t, applied)
	// One SetElements transaction per policy, in deterministic sorted name order.
	require.Equal(t, []string{"bn-partner", "bn-publisher", "bn-restricted"}, r.setElemOrder)
}

func TestApplyMembership_FullListReplace(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32", "10.1.0.2/32"}, "10.4.0.0/24")

	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-publisher": {"10.5.0.0/24"},
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"10.5.0.0/24"}, r.elements["bn-publisher"],
		"apply is a full-list replace like `set`, not an append")
}

func TestApplyMembership_EmptyCIDRsClearsSet(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24")

	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-publisher": {},
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, r.elements["bn-publisher"], "an empty desired list clears the policy's set")
}

func TestApplyMembership_EmptyMapIsNoopAndTakesNoLock(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	// Pre-hold the shared flock from the test. If ApplyMembership tried to
	// acquire it for an empty batch, it would report skipped (false); instead
	// it must short-circuit to (true, nil) without touching the lock at all.
	lf, err := os.OpenFile(lockPathFor(nftPath), os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer func() { _ = lf.Close() }()
	require.NoError(t, syscall.Flock(int(lf.Fd()), syscall.LOCK_EX))
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()

	applied, err := m.ApplyMembership(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, applied, "an empty batch is a no-op success, independent of the lock")
	require.Empty(t, r.setElemOrder, "no policy transactions for an empty batch")
}

func TestApplyMembership_SkipsWhenLockHeld(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	// Simulate a hand-run operator apply holding the shared flock (a separate
	// open-file-description on the same lock path, as a concurrent process
	// would have). The daemon's non-blocking acquisition must fail cleanly.
	lf, err := os.OpenFile(lockPathFor(nftPath), os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer func() { _ = lf.Close() }()
	require.NoError(t, syscall.Flock(int(lf.Fd()), syscall.LOCK_EX))
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()

	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-publisher": {"10.1.0.1/32"},
	})
	require.NoError(t, err, "a held lock is not an error — the tick is skipped")
	require.False(t, applied, "batch is skipped while an operator apply holds the lock")
	require.Empty(t, r.setElemOrder, "nothing is written when the tick is skipped")
	require.Empty(t, r.elements["bn-publisher"])
}

func TestApplyMembership_NeverCreatesMissingPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	applied, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-nonexistent": {"10.0.0.1/32"},
	})
	require.ErrorContains(t, err, "not found")
	require.False(t, applied)
	require.Zero(t, r.applyCount, "the apply path must never call create/Apply")
	require.Empty(t, r.setElemOrder)
}

func TestApplyMembership_TableMissingReturnsError(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	require.NoError(t, r.Delete(context.Background()))

	_, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-publisher": {"10.1.0.1/32"},
	})
	require.ErrorContains(t, err, "policy table not found")
}

func TestApplyMembership_PartialBatchCommitsPriorPolicies(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	// "bn-publisher" sorts before "bn-zzz-missing", so it is applied before the
	// batch hits the missing policy and stops. Prior work stays committed; the
	// caller re-drives on the next tick (the apply is idempotent).
	_, err := m.ApplyMembership(context.Background(), map[string][]string{
		"bn-publisher":   {"10.1.0.1/32"},
		"bn-zzz-missing": {"10.2.0.2/32"},
	})
	require.ErrorContains(t, err, "not found")
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"],
		"policies applied before the failing one remain committed")
}

func TestApplyMembership_IdenticalKernelStateToLoopedSet(t *testing.T) {
	desired := map[string][]string{
		"bn-publisher":  {"10.1.0.1/32", "10.1.0.2/32"},
		"bn-backfill":   {"10.30.5.7:43473"},
		"bn-restricted": {"10.99.0.0/16"},
	}

	seedAll := func(m *Manager) {
		seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
		seedDenyPolicy(t, m, "bn-restricted", nil)
		_, err := m.Create(context.Background(),
			&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			nil, "10.4.0.0/24", false)
		require.NoError(t, err)
	}

	// Path A: the daemon's batched ApplyMembership.
	rBatch := newFakeRunner()
	mBatch, _, _ := newTestManager(t, rBatch)
	seedAll(mBatch)
	applied, err := mBatch.ApplyMembership(context.Background(), desired)
	require.NoError(t, err)
	require.True(t, applied)

	// Path B: the hand-run CLI verb, looped over the same input.
	rLoop := newFakeRunner()
	mLoop, _, _ := newTestManager(t, rLoop)
	seedAll(mLoop)
	for name, cidrs := range desired {
		require.NoError(t, mLoop.Set(context.Background(), name, cidrs))
	}

	require.Equal(t, rLoop.elements, rBatch.elements,
		"monitor and CLI apply paths must produce identical live-set state")
}
