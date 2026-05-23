// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
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
	f, err := os.OpenFile(path, flag, 0o640)
	if err != nil {
		return nil, err
	}
	return &EventLogger{path: path, file: f}, nil
}

// Log appends one JSON line to the file and fsyncs.
// Returns an error if marshalling or I/O fails; the caller decides whether to halt or continue.
func (l *EventLogger) Log(e Event) error {
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

// PruneOldest applies the retention policy to per-operation JSONL files matching
// glob in dir: first removes files older than maxAge, then removes oldest-first
// until at most keep files remain. Called on daemon and UC startup.
func PruneOldest(dir, glob string, maxAge time.Duration, keep int) error {
	matches, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return err
	}

	// Sort ascending by name — ISO-8601 timestamps in filenames sort chronologically.
	sort.Strings(matches)

	cutoff := time.Now().Add(-maxAge)

	// Pass 1: remove files older than maxAge.
	var remaining []string
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil {
			continue // already gone
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(p)
		} else {
			remaining = append(remaining, p)
		}
	}

	// Pass 2: if still over the cap, remove oldest first.
	for len(remaining) > keep {
		_ = os.Remove(remaining[0])
		remaining = remaining[1:]
	}

	return nil
}
