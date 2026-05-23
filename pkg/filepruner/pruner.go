// SPDX-License-Identifier: Apache-2.0

package filepruner

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Strategy decides whether a file is a candidate for pruning.
// If ShouldPrune returns an error the file is kept — the pruner will not delete
// a file whose eligibility cannot be determined.
type Strategy interface {
	ShouldPrune(path string) (bool, error)
}

// Pruner removes files matching a glob pattern according to a strategy-driven
// age filter followed by a hard-cap limit.
type Pruner struct {
	strategy Strategy
}

// New returns a Pruner that uses strategy to decide which files to prune.
func New(strategy Strategy) *Pruner {
	return &Pruner{strategy: strategy}
}

// Prune applies the retention policy to files matching glob in dir:
//  1. Files for which the strategy returns ShouldPrune=true are removed.
//  2. If more than keep files remain, the oldest (first in ascending name order)
//     are removed until the cap is satisfied.
//
// Files are sorted ascending by name before both passes. For strategies whose
// decision is based on a timestamp embedded in the filename
// (e.g. FilenameTimestampStrategy) this preserves chronological order.
//
// Returns a combined error if any deletion fails, so partial pruning is visible
// to the caller rather than silently violating the retention contract.
func (p *Pruner) Prune(dir, glob string, keep int) error {
	matches, err := globSorted(dir, glob)
	if err != nil {
		return err
	}

	var errs []error
	var remaining []string

	for _, path := range matches {
		candidate, stratErr := p.strategy.ShouldPrune(path)
		if stratErr != nil {
			// Cannot determine eligibility — keep the file rather than silently delete it.
			remaining = append(remaining, path)
			continue
		}
		if candidate {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				errs = append(errs, ErrPruneFailed.Wrap(rmErr, "remove %s", path))
			}
		} else {
			remaining = append(remaining, path)
		}
	}

	return errors.Join(append(errs, enforceCap(remaining, keep)...)...)
}

// FilenameTimestampStrategy prunes files whose timestamp embedded in the filename
// is older than MaxAge. The timestamp is extracted using the provided Layout
// (Go time format string), e.g. "20060102T150405Z" for compact ISO-8601 with UTC suffix.
// Intended for per-operation files such as
// "consensus-upgrade-20260415T143000Z-v0.75.0.jsonl".
// Files with no parseable timestamp are not pruned.
type FilenameTimestampStrategy struct {
	Layout string
	MaxAge time.Duration
}

// ShouldPrune returns true if the timestamp embedded in the filename predates now-MaxAge.
func (s FilenameTimestampStrategy) ShouldPrune(path string) (bool, error) {
	name := filepath.Base(path)
	n := len(s.Layout)
	for i := 0; i <= len(name)-n; i++ {
		t, err := time.Parse(s.Layout, name[i:i+n])
		if err == nil {
			return t.Before(time.Now().Add(-s.MaxAge)), nil
		}
	}
	return false, ErrNoTimestamp.New("no %q timestamp found in filename %q", s.Layout, name)
}

// ModTimeStrategy prunes files whose modification time is older than MaxAge.
// Suitable for files that do not embed a timestamp in their name.
type ModTimeStrategy struct {
	MaxAge time.Duration
}

// ShouldPrune returns true if the file's ModTime predates now-MaxAge.
func (s ModTimeStrategy) ShouldPrune(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.ModTime().Before(time.Now().Add(-s.MaxAge)), nil
}

// FileSizeStrategy prunes files whose size exceeds MaxBytes.
// Useful for unbounded append-only files that have no age-based retention policy.
type FileSizeStrategy struct {
	MaxBytes int64
}

// ShouldPrune returns true if the file's size exceeds MaxBytes.
func (s FileSizeStrategy) ShouldPrune(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.Size() > s.MaxBytes, nil
}

// All returns a composite strategy that prunes a file only when every provided
// strategy returns ShouldPrune=true (logical AND). The first error encountered
// is returned and the file is kept.
func All(strategies ...Strategy) Strategy {
	return allStrategy(strategies)
}

type allStrategy []Strategy

func (a allStrategy) ShouldPrune(path string) (bool, error) {
	for _, s := range a {
		ok, err := s.ShouldPrune(path)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Any returns a composite strategy that prunes a file when at least one provided
// strategy returns ShouldPrune=true (logical OR). The first error encountered
// is returned and the file is kept.
func Any(strategies ...Strategy) Strategy {
	return anyStrategy(strategies)
}

type anyStrategy []Strategy

func (a anyStrategy) ShouldPrune(path string) (bool, error) {
	for _, s := range a {
		ok, err := s.ShouldPrune(path)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
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
