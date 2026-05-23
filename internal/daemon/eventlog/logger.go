// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// EventLogger appends JSON lines to a single file, safe for concurrent use.
// Each Log call fsyncs so entries survive a daemon crash.
type EventLogger struct {
	mu   sync.Mutex
	path string
	file *os.File
}

// Path returns the absolute path of the underlying JSONL file.
func (l *EventLogger) Path() string { return l.path }

// NewOperation creates a per-operation JSONL file in dir named
// "consensus-<operationID>.jsonl" and returns a logger. The file is truncated
// on open so each operation starts fresh. The caller must call Close when done.
func NewOperation(dir, operationID string) (*EventLogger, error) {
	return open(filepath.Join(dir, "consensus-"+operationID+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
}

// NewAppend opens (or creates) a fixed append-only JSONL file in dir named
// fileName. Used for migration events where multiple operations share one file.
func NewAppend(dir, fileName string) (*EventLogger, error) {
	return open(filepath.Join(dir, fileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND)
}

func open(path string, flag int) (*EventLogger, error) {
	// Resolve to absolute path so Path() always returns an absolute path
	// regardless of whether the caller passed a relative dir.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs, flag, 0o640)
	if err != nil {
		return nil, err
	}
	return &EventLogger{path: abs, file: f}, nil
}

// Log validates e, appends one JSON line to the file, and fsyncs.
// Returns an error if any required field is empty, or if marshalling or I/O fails.
// The caller decides whether to halt or continue on error.
func (l *EventLogger) Log(e Event) error {
	if err := e.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.file.Write(b); err != nil {
		return err
	}
	return l.file.Sync()
}

// Close flushes and closes the underlying file.
func (l *EventLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// filenameTimestampLayout is the ISO-8601 compact form embedded in per-operation filenames,
// e.g. "consensus-upgrade-20260415T143000-v0.75.0.jsonl" → "20260415T143000".
const filenameTimestampLayout = "20060102T150405"

// PruneOldest applies the retention policy to per-operation JSONL files matching
// glob in dir using the ISO-8601 timestamp embedded in each filename to determine
// age. Files older than maxAge are removed first; then oldest-first until at most
// keep files remain. Returns a combined error if any deletion fails.
//
// Use this for per-operation files whose names embed a timestamp
// (e.g. "consensus-upgrade-20260415T143000-v0.75.0.jsonl").
// Files whose names contain no parseable timestamp are kept and count toward the cap.
// For files without a timestamp in the name use PruneOldestByModTime instead.
func PruneOldest(dir, glob string, maxAge time.Duration, keep int) error {
	matches, err := globSorted(dir, glob)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	var errs []error
	var remaining []string

	for _, p := range matches {
		ts, parseErr := parseFilenameTimestamp(filepath.Base(p))
		if parseErr != nil {
			// Cannot determine age from filename — keep rather than silently delete.
			remaining = append(remaining, p)
			continue
		}
		if ts.Before(cutoff) {
			if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s: %w", p, rmErr))
			}
		} else {
			remaining = append(remaining, p)
		}
	}

	return errors.Join(append(errs, enforceCap(remaining, keep)...)...)
}

// PruneOldestByModTime applies the retention policy to JSONL files matching glob
// in dir using each file's modification time to determine age. Files whose ModTime
// is older than maxAge are removed first; then oldest-first (by ModTime) until at
// most keep files remain. Returns a combined error if any deletion fails.
//
// Use this for files without a timestamp in the filename
// (e.g. a fixed append-only file that has been rotated externally).
// For per-operation files with an embedded filename timestamp use PruneOldest instead.
func PruneOldestByModTime(dir, glob string, maxAge time.Duration, keep int) error {
	matches, err := globSorted(dir, glob)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	var errs []error
	var remaining []string

	for _, p := range matches {
		info, statErr := os.Stat(p)
		if statErr != nil {
			continue // already gone
		}
		if info.ModTime().Before(cutoff) {
			if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s: %w", p, rmErr))
			}
		} else {
			remaining = append(remaining, p)
		}
	}

	return errors.Join(append(errs, enforceCap(remaining, keep)...)...)
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

// enforceCap removes the oldest files (front of the sorted slice) until len(files) <= keep.
func enforceCap(files []string, keep int) []error {
	var errs []error
	for len(files) > keep {
		if rmErr := os.Remove(files[0]); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", files[0], rmErr))
		}
		files = files[1:]
	}
	return errs
}

// parseFilenameTimestamp extracts the ISO-8601 compact timestamp from a filename.
// Returns an error if no parseable timestamp is found.
func parseFilenameTimestamp(name string) (time.Time, error) {
	for i := 0; i < len(name); i++ {
		// A compact ISO-8601 timestamp is exactly 15 chars: YYYYMMDDTHHmmSS
		if i+15 <= len(name) {
			t, err := time.Parse(filenameTimestampLayout, name[i:i+15])
			if err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no parseable timestamp in filename %q", name)
}
