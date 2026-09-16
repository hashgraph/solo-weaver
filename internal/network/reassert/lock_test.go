// SPDX-License-Identifier: Apache-2.0

package reassert

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// holdFlock takes an exclusive flock on path from a second file description and
// keeps it until the test ends, standing in for a manager mid-apply. flock
// attaches to the open file description, not the process, so this contends with
// flockNB even from the same test binary.
func holdFlock(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB),
		"the test could not take the lock it means to hold")
	t.Cleanup(func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	})
}

func TestFlockNB_AcquiresAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".applying")

	release, acquired, err := flockNB(path)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)

	release()

	// The lock must be free again, or a second run on the host would skip forever.
	release2, acquired2, err := flockNB(path)
	require.NoError(t, err)
	assert.True(t, acquired2, "a released lock must be re-acquirable")
	release2()
}

// TestFlockNB_HeldElsewhereIsBenignContention is the contract behind the
// "skipped" state: a held lock must come back with a nil error, since an error
// would report the plane as unprobeable damage instead of an operator apply.
func TestFlockNB_HeldElsewhereIsBenignContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".applying")
	holdFlock(t, path)

	release, acquired, err := flockNB(path)

	require.NoError(t, err, "contention is not a fault")
	assert.False(t, acquired)
	require.NotNil(t, release, "the release func must be safe to call even when the lock was not taken")
	release()

	// And the reported outcome is a skip, not a probe failure.
	st, skip := planeLock{path: path, acquired: acquired, err: err}.skip(ArtifactHostFirewall)
	require.True(t, skip)
	assert.True(t, st.Skipped)
	assert.False(t, st.ProbeFailed)
}

func TestFlockNB_CreatesTheLockDirectory(t *testing.T) {
	// /run/solo-provisioner/network does not survive a reboot, so the first run
	// after boot must create it rather than report a fault.
	path := filepath.Join(t.TempDir(), "solo-provisioner", "network", ".applying")

	release, acquired, err := flockNB(path)

	require.NoError(t, err)
	assert.True(t, acquired)
	release()
	assert.FileExists(t, path)
}

func TestFlockNB_FilesystemFailureIsAnError(t *testing.T) {
	// A file where the lock directory should be: the lock can never be taken
	// here, and that must surface as a fault rather than as benign contention.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "network")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))

	release, acquired, err := flockNB(filepath.Join(blocker, ".applying"))

	require.Error(t, err)
	assert.False(t, acquired)
	require.NotNil(t, release)
	release()

	// A fault is reported as unprobeable, never as a skip.
	st, skip := planeLock{acquired: acquired, err: err}.skip(ArtifactHostFirewall)
	require.True(t, skip)
	assert.True(t, st.ProbeFailed)
	assert.False(t, st.Skipped)
}

// TestReassert_RealFlockContentionSkipsTheLockedPlane exercises the production
// lock seam end to end: every other test injects AcquireLock, so this is the
// only proof that a lock actually held on disk yields the plane.
func TestReassert_RealFlockContentionSkipsTheLockedPlane(t *testing.T) {
	dir := t.TempDir()
	nftLock := filepath.Join(dir, ".applying")
	shapeLock := filepath.Join(dir, ".tc-applying")

	k := &fakeKernel{restartFixes: true}
	cfg := testConfig(k, allProvisioned())
	cfg.NftLockPath = nftLock
	cfg.ShapeLockPath = shapeLock
	// nil means the production flockNB, not the fake.
	cfg.AcquireLock = nil

	holdFlock(t, nftLock)

	r := NewWithConfig(cfg).Reassert(context.Background())

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		a := artifact(t, r, id)
		assert.True(t, a.Skipped, "%s must yield to the operator apply holding %s", id, nftLock)
		assert.False(t, a.Reasserted)
	}
	assert.Zero(t, k.nftRestarts, "the loader must not be restarted under a real held lock")

	// The uncontended plane still recovers, so one apply cannot stall the rest.
	assert.True(t, artifact(t, r, ArtifactEgressQdisc).Reasserted)
	assert.Equal(t, 1, k.shaperRestarts)

	// And the run left the shaper lock free for the next operator command.
	release, acquired, err := flockNB(shapeLock)
	require.NoError(t, err)
	assert.True(t, acquired, "Reassert must not leak the shaper flock")
	release()
}

// TestReassertLocksThePathsTheManagersLock is a correctness guard, not coverage.
// The default nft lock is taken from the policy package, but the firewall manager
// locks its own constant — two separate declarations that happen to hold the same
// string. Change either and reassert silently stops serialising against firewall
// applies, with no compile error anywhere.
func TestReassertLocksThePathsTheManagersLock(t *testing.T) {
	require.Equal(t, firewall.LockPath, policy.LockPath,
		"the firewall and policy managers must share one apply lock for reassert to serialise against both")

	r := NewWithConfig(Config{})
	assert.Equal(t, firewall.LockPath, r.nftLockPath, "reassert must take the lock the nft managers take")
	assert.Equal(t, shape.ShapeLockPath, r.shapeLockPath, "reassert must take the lock the shaper takes")
}
