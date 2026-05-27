// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
// operationID must not contain path separators or be an absolute path.
func NewOperation(dir, operationID string) (*EventLogger, error) {
	if filepath.Base(operationID) != operationID || filepath.IsAbs(operationID) {
		return nil, ErrInvalidEvent.New("operationID contains path separators or is absolute: %q", operationID)
	}
	return open(filepath.Join(dir, "consensus-"+operationID+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
}

// NewAppend opens (or creates) a fixed append-only JSONL file in dir named
// fileName. Used for migration events where multiple operations share one file.
// fileName must be a plain filename with no path separators.
func NewAppend(dir, fileName string) (*EventLogger, error) {
	if filepath.Base(fileName) != fileName || filepath.IsAbs(fileName) {
		return nil, ErrInvalidEvent.New("fileName must be a plain filename with no path separators: %q", fileName)
	}
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
