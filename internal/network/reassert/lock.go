// SPDX-License-Identifier: Apache-2.0

package reassert

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/joomcode/errorx"
)

// planeLock is the outcome of taking one plane's apply lock: acquired, held by
// someone else (acquired=false), or failed (err set).
type planeLock struct {
	path     string
	acquired bool
	err      error
	release  func()
}

// openPlaneLock is a lock that is always acquired. Check uses it because it
// changes nothing, so it does not need a real lock.
func openPlaneLock() planeLock {
	return planeLock{acquired: true, release: func() {}}
}

// skip reports whether this plane must be left alone this run, and the status
// to report for its artifact if so.
func (l planeLock) skip(id string) (ArtifactStatus, bool) {
	switch {
	case l.err != nil:
		return ArtifactStatus{
			Artifact:    id,
			ProbeFailed: true,
			Detail:      "cannot determine: " + l.err.Error(),
		}, true
	case !l.acquired:
		return ArtifactStatus{
			Artifact: id,
			Skipped:  true,
			Detail:   "skipped: an apply is in progress (" + l.path + " is held)",
		}, true
	default:
		return ArtifactStatus{}, false
	}
}

// lockPlane takes one plane's apply lock without blocking. The release func is
// always safe to call.
func (r *Reasserter) lockPlane(path string) planeLock {
	release, acquired, err := r.acquireLock(path)
	if release == nil {
		release = func() {}
	}
	return planeLock{path: path, acquired: acquired, err: err, release: release}
}

// flockNB takes an exclusive, non-blocking flock on path. A lock held elsewhere
// returns (noop, false, nil); only a filesystem failure returns an error.
func flockNB(path string) (func(), bool, error) {
	noop := func() {}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return noop, false, errorx.ExternalError.Wrap(err, "failed to create lock directory %s", dir)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return noop, false, errorx.ExternalError.Wrap(err, "failed to open lock file %s", path)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// EWOULDBLOCK means the lock is held; it equals EAGAIN on our platforms.
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return noop, false, nil
		}
		return noop, false, errorx.ExternalError.Wrap(err, "failed to acquire lock %s", path)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}
