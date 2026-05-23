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
// glob in dir: first removes files whose embedded filename timestamp is older than
// maxAge, then removes oldest-first until at most keep files remain.
// Returns a combined error if any deletion fails — partial pruning is reported so
// the caller can log a warning rather than silently violating the retention contract.
//
// This function is intended only for per-operation files whose names embed an
// ISO-8601 timestamp (e.g. "consensus-upgrade-20260415T143000-v0.75.0.jsonl").
// Fixed append-only files such as "consensus-migrate-events.jsonl" have no
// retention policy and must never be passed to this function — they carry no
// timestamp in the filename and would be skipped by the age check but still count
// toward the hard cap, producing unexpected deletions.
func PruneOldest(dir, glob string, maxAge time.Duration, keep int) error {
	matches, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return err
	}

	// Sort ascending by name — ISO-8601 timestamps in filenames sort chronologically.
	sort.Strings(matches)

	cutoff := time.Now().Add(-maxAge)
	var errs []error

	// Pass 1: remove files whose filename timestamp predates the cutoff.
	var remaining []string
	for _, p := range matches {
		ts, parseErr := parseFilenameTimestamp(filepath.Base(p))
		if parseErr != nil {
			// Cannot determine age from filename — keep the file rather than
			// silently deleting something we can't date.
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

	// Pass 2: if still over the cap, remove oldest first.
	for len(remaining) > keep {
		if rmErr := os.Remove(remaining[0]); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", remaining[0], rmErr))
		}
		remaining = remaining[1:]
	}

	return errors.Join(errs...)
}

// parseFilenameTimestamp extracts the ISO-8601 compact timestamp from a filename
// of the form "consensus-upgrade-<ts>-<ver>.jsonl" (e.g. "20260415T143000").
// Returns an error if no parseable timestamp is found.
func parseFilenameTimestamp(name string) (time.Time, error) {
	// Filenames follow the pattern: prefix-<YYYYMMDDTHHmmSS>-<suffix>
	// Walk through the dash-separated segments and try to parse each one.
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
