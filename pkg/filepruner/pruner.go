// SPDX-License-Identifier: Apache-2.0

package filepruner

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

)

// Strategy determines the reference timestamp used to assess a file's age.
// If Timestamp returns an error the file is kept — the pruner will not delete
// a file whose age cannot be determined.
type Strategy interface {
	Timestamp(path string) (time.Time, error)
}

// Pruner removes files matching a glob pattern according to an age + hard-cap
// retention policy. The age of each file is determined by the injected Strategy.
type Pruner struct {
	strategy Strategy
}

// New returns a Pruner that uses strategy to determine file age.
func New(strategy Strategy) *Pruner {
	return &Pruner{strategy: strategy}
}

// Prune applies the retention policy to files matching glob in dir:
//  1. Files whose strategy-determined timestamp is older than maxAge are removed.
//  2. If more than keep files remain, the oldest are removed first until the cap
//     is satisfied.
//
// Files are sorted ascending by name before both passes; for strategies whose
// timestamp is embedded in the filename (e.g. FilenameTimestampStrategy) this
// preserves chronological order for the cap pass. For ModTimeStrategy the sort
// is still by name — chronological order for the cap pass depends on filenames
// being written in time order, which is true for per-operation files.
//
// Returns a combined error if any deletion fails, so partial pruning is visible
// to the caller rather than silently violating the retention contract.
func (p *Pruner) Prune(dir, glob string, maxAge time.Duration, keep int) error {
	matches, err := globSorted(dir, glob)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	var errs []error
	var remaining []string

	for _, path := range matches {
		ts, tsErr := p.strategy.Timestamp(path)
		if tsErr != nil {
			// Cannot determine age — keep the file rather than silently delete it.
			remaining = append(remaining, path)
			continue
		}
		if ts.Before(cutoff) {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				errs = append(errs, ErrPruneFailed.Wrap(rmErr, "remove %s", path))
			}
		} else {
			remaining = append(remaining, path)
		}
	}

	return errors.Join(append(errs, enforceCap(remaining, keep)...)...)
}

// FilenameTimestampStrategy parses the file's age from a timestamp embedded in
// its filename using the provided Layout (Go time format string).
// Intended for per-operation files such as
// "consensus-upgrade-20260415T143000Z-v0.75.0.jsonl".
type FilenameTimestampStrategy struct {
	// Layout is the Go time format string for the timestamp embedded in the filename,
	// e.g. "20060102T150405Z" for compact ISO-8601 with UTC suffix.
	Layout string
}

// Timestamp scans the filename for the first substring that parses with Layout.
// Returns an error if no parseable timestamp is found.
func (s FilenameTimestampStrategy) Timestamp(path string) (time.Time, error) {
	name := filepath.Base(path)
	n := len(s.Layout)
	for i := 0; i <= len(name)-n; i++ {
		t, err := time.Parse(s.Layout, name[i:i+n])
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, ErrNoTimestamp.New("no %q timestamp found in filename %q", s.Layout, name)
}

// ModTimeStrategy uses the file's modification time as its age reference.
// Suitable for files that do not embed a timestamp in their name.
type ModTimeStrategy struct{}

// Timestamp returns the file's ModTime.
func (ModTimeStrategy) Timestamp(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// globSorted returns files matching glob in dir, sorted ascending by name.
func globSorted(dir, glob string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// enforceCap removes the oldest files (front of the sorted slice) until
// len(files) <= keep. Returns any deletion errors.
func enforceCap(files []string, keep int) []error {
	var errs []error
	for len(files) > keep {
		if rmErr := os.Remove(files[0]); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			errs = append(errs, ErrPruneFailed.Wrap(rmErr, "remove %s", files[0]))
		}
		files = files[1:]
	}
	return errs
}
